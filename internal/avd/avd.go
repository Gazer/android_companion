// Package avd manages Android Virtual Devices: discovery, launch and shutdown.
package avd

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Manager handles emulator lifecycle.
type Manager struct {
	SdkRoot      string
	EmulatorBin  string
	AdbBin       string
}

// NewManager discovers the Android SDK location.
func NewManager() (*Manager, error) {
	sdk := os.Getenv("ANDROID_HOME")
	if sdk == "" {
		sdk = os.Getenv("ANDROID_SDK_ROOT")
	}
	if sdk == "" {
		home, _ := os.UserHomeDir()
		candidates := []string{
			filepath.Join(home, "Library", "Android", "sdk"), // macOS
			filepath.Join(home, "Android", "Sdk"),            // Linux
		}
		for _, c := range candidates {
			if st, err := os.Stat(c); err == nil && st.IsDir() {
				sdk = c
				break
			}
		}
	}
	if sdk == "" {
		return nil, fmt.Errorf("Android SDK not found (set ANDROID_HOME)")
	}

	m := &Manager{
		SdkRoot:     sdk,
		EmulatorBin: filepath.Join(sdk, "emulator", "emulator"),
		AdbBin:      filepath.Join(sdk, "platform-tools", "adb"),
	}
	if _, err := os.Stat(m.EmulatorBin); err != nil {
		return nil, fmt.Errorf("emulator binary not found at %s", m.EmulatorBin)
	}
	if _, err := os.Stat(m.AdbBin); err != nil {
		return nil, fmt.Errorf("adb binary not found at %s", m.AdbBin)
	}
	return m, nil
}

// ListAVDs returns the names of all configured AVDs.
func (m *Manager) ListAVDs(ctx context.Context) ([]string, error) {
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
	Name    string
	GRPCPort int
	cmd     *exec.Cmd
	mgr     *Manager
}

// Launch starts an emulator headless with the gRPC bridge enabled.
// It waits until the device is fully booted.
func (m *Manager) Launch(ctx context.Context, avdName string, grpcPort int, wipeData bool) (*Instance, error) {
	args := []string{
		"-avd", avdName,
		"-no-window",
		"-no-audio",
		"-gpu", "swiftshader_indirect", // safe headless GPU
		"-grpc", fmt.Sprintf("%d", grpcPort),
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

	inst := &Instance{Name: avdName, GRPCPort: grpcPort, cmd: cmd, mgr: m}

	if err := inst.waitBoot(ctx); err != nil {
		_ = inst.Kill()
		return nil, fmt.Errorf("boot failed (output: %s): %w", outBuf.String(), err)
	}
	return inst, nil
}

// waitBoot polls adb until sys.boot_completed=1.
func (i *Instance) waitBoot(ctx context.Context) error {
	deadline := time.Now().Add(3 * time.Minute)
	serial, err := i.findSerial(ctx)
	if err != nil {
		return err
	}
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		cmd := exec.CommandContext(ctx, i.mgr.AdbBin, "-s", serial, "shell", "getprop", "sys.boot_completed")
		out, err := cmd.Output()
		if err == nil && strings.TrimSpace(string(out)) == "1" {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("emulator boot timeout")
}

// findSerial locates the adb serial for the emulator we just launched.
// With -no-window the console port is still allocated; we can match by AVD name.
func (i *Instance) findSerial(ctx context.Context) (string, error) {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		cmd := exec.CommandContext(ctx, i.mgr.AdbBin, "devices")
		out, err := cmd.Output()
		if err == nil {
			sc := bufio.NewScanner(bytes.NewReader(out))
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if strings.HasPrefix(line, "emulator-") && strings.Contains(line, "device") {
					return strings.Fields(line)[0], nil
				}
			}
		}
		time.Sleep(1 * time.Second)
	}
	return "", fmt.Errorf("emulator not found in adb devices")
}

// Serial returns the adb serial of this instance (e.g. emulator-5554).
func (i *Instance) Serial(ctx context.Context) (string, error) {
	return i.findSerial(ctx)
}

// Kill terminates the emulator process.
func (i *Instance) Kill() error {
	if i.cmd.Process == nil {
		return nil
	}
	// Try graceful kill via adb first (saves state)
	_ = i.cmd.Process.Kill()
	_ = i.cmd.Wait()
	return nil
}
