// Package avd manages Android Virtual Devices: discovery, launch and shutdown.
package avd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rmarkiewicz/android-companion/internal/adb"
)

// Emulator console ports: the emulator accepts even ports in this range,
// and its adb serial is "emulator-<port>".
const (
	firstConsolePort = 5554
	lastConsolePort  = 5682
)

// Manager locates the SDK tools and handles the emulator lifecycle.
type Manager struct {
	SdkRoot string
	// EmulatorBin is empty when the Android Emulator isn't installed: only
	// devices connected to adb can be used then.
	EmulatorBin string
	AdbBin      string
}

// NewManager discovers the Android SDK tools. Only adb is required: it's
// taken from the SDK's platform-tools, or from PATH.
func NewManager() (*Manager, error) {
	m := &Manager{SdkRoot: findSDK()}
	if m.SdkRoot != "" {
		if p := filepath.Join(m.SdkRoot, "platform-tools", "adb"); isFile(p) {
			m.AdbBin = p
		}
		if p := filepath.Join(m.SdkRoot, "emulator", "emulator"); isFile(p) {
			m.EmulatorBin = p
		}
	}
	if m.AdbBin == "" {
		if p, err := exec.LookPath("adb"); err == nil {
			m.AdbBin = p
		}
	}
	if m.AdbBin == "" {
		return nil, errors.New("adb not found: install the Android SDK platform-tools (set ANDROID_HOME) or add adb to PATH")
	}
	return m, nil
}

func findSDK() string {
	for _, env := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if sdk := os.Getenv(env); sdk != "" {
			return sdk
		}
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "Library", "Android", "sdk"), // macOS
		filepath.Join(home, "Android", "Sdk"),            // Linux
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	return ""
}

func isFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// HasEmulator reports whether AVDs can be listed and launched.
func (m *Manager) HasEmulator() bool { return m.EmulatorBin != "" }

// ListAVDs returns the names of all configured AVDs (none if the emulator
// isn't installed).
func (m *Manager) ListAVDs(ctx context.Context) ([]string, error) {
	if !m.HasEmulator() {
		return nil, nil
	}
	cmd := exec.CommandContext(ctx, m.EmulatorBin, "-list-avds")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list avds: %w", err)
	}
	var avds []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" && !strings.HasPrefix(line, "INFO") {
			avds = append(avds, line)
		}
	}
	return avds, nil
}

// Instance represents a running emulator process.
type Instance struct {
	Name     string
	GRPCPort int
	// Serial is the emulator's adb serial ("emulator-<console port>").
	Serial string
	cmd    *exec.Cmd
	mgr    *Manager
}

// Launch starts an emulator headless with the gRPC bridge enabled.
// It waits until the device is fully booted.
func (m *Manager) Launch(ctx context.Context, avdName string, grpcPort int, wipeData bool) (*Instance, error) {
	if !m.HasEmulator() {
		return nil, errors.New("the Android Emulator is not installed")
	}
	// An explicit console port makes the adb serial known in advance, so
	// other running emulators can't be mistaken for this one.
	port, err := m.freeConsolePort(ctx)
	if err != nil {
		return nil, err
	}
	args := []string{
		"-avd", avdName,
		"-port", strconv.Itoa(port),
		"-no-window",
		"-no-audio",
		"-gpu", "swiftshader_indirect", // safe headless GPU
		"-grpc", strconv.Itoa(grpcPort),
	}
	if wipeData {
		args = append(args, "-wipe-data")
	} else {
		args = append(args, "-no-snapshot-load")
	}
	cmd := exec.CommandContext(ctx, m.EmulatorBin, args...)
	// Capture output to debug launch issues
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start emulator: %w", err)
	}

	inst := &Instance{
		Name:     avdName,
		GRPCPort: grpcPort,
		Serial:   "emulator-" + strconv.Itoa(port),
		cmd:      cmd,
		mgr:      m,
	}

	if err := inst.waitBoot(ctx); err != nil {
		_ = inst.Kill()
		return nil, fmt.Errorf("boot failed (output: %s): %w", outBuf.String(), err)
	}
	return inst, nil
}

// freeConsolePort returns the first emulator console port not used by a
// running emulator nor by another process.
func (m *Manager) freeConsolePort(ctx context.Context) (int, error) {
	used := make(map[string]bool)
	if devs, err := adb.ListDevices(ctx, m.AdbBin); err == nil {
		for _, d := range devs {
			used[d.Serial] = true
		}
	}
	for port := firstConsolePort; port <= lastConsolePort; port += 2 {
		if used["emulator-"+strconv.Itoa(port)] {
			continue
		}
		// The emulator also uses port+1 for adb.
		if portFree(port) && portFree(port+1) {
			return port, nil
		}
	}
	return 0, errors.New("no free emulator console port")
}

func portFree(port int) bool {
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return false
	}
	_ = l.Close()
	return true
}

// waitBoot polls adb until sys.boot_completed=1.
func (i *Instance) waitBoot(ctx context.Context) error {
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		cmd := exec.CommandContext(ctx, i.mgr.AdbBin, "-s", i.Serial, "shell", "getprop", "sys.boot_completed")
		out, err := cmd.Output()
		if err == nil && strings.TrimSpace(string(out)) == "1" {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("emulator boot timeout")
}

// Kill terminates the emulator process.
func (i *Instance) Kill() error {
	if i.cmd.Process == nil {
		return nil
	}
	_ = i.cmd.Process.Kill()
	_ = i.cmd.Wait()
	return nil
}
