// Package scrcpy mirrors an Android device with scrcpy's server (v4.0,
// embedded in the binary): the device streams its screen as H.264 and
// accepts input events. See https://github.com/Genymobile/scrcpy
// (doc/develop.md) for the protocol, which is pinned to the server version.
package scrcpy

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// serverJar is scrcpy-server v4.0 (Apache License 2.0, see LICENSE),
// sha256 84924bd564a1eb6089c872c7521f968058977f91f5ff02514a8c74aff3210f3a.
//
//go:embed scrcpy-server-v4.0
var serverJar []byte

const (
	// serverVersion must match serverJar: the server refuses other clients.
	serverVersion = "4.0"
	devicePath    = "/data/local/tmp/android-companion-scrcpy-server.jar"

	startTimeout   = 20 * time.Second
	controlBacklog = 256
)

// Options configures a mirroring session.
type Options struct {
	AdbPath string
	Serial  string
	// MaxSize limits the largest frame dimension (0: device resolution).
	MaxSize int
	// MaxFPS limits the frame rate (0: no limit).
	MaxFPS int
	// BitRate in bits per second (0: server default, 8 Mbps).
	BitRate int
}

// Session is a running scrcpy server with its video and control sockets.
type Session struct {
	// DeviceName is the name reported by the server (e.g. "Pixel 5").
	DeviceName string

	video   net.Conn
	control net.Conn
	cmd     *exec.Cmd
	output  *tailBuffer
	exited  chan struct{}
	buf     []byte

	ctlMu     sync.Mutex
	ctl       chan []byte
	closed    bool
	closeOnce sync.Once
}

// Start pushes the server to the device, starts it and connects to it.
// ctx bounds the startup only; call Close to stop the session.
func Start(ctx context.Context, o Options) (*Session, error) {
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	if err := push(ctx, o); err != nil {
		return nil, err
	}

	// The device connects to us through `adb reverse` (scrcpy's default:
	// no polling needed, the server connects once it's ready).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer ln.Close()
	scid := rand.Uint32() & 0x7fffffff // 31-bit, identifies our socket
	socket := fmt.Sprintf("localabstract:scrcpy_%08x", scid)
	port := ln.Addr().(*net.TCPAddr).Port
	if out, err := adb(ctx, o, "reverse", socket, "tcp:"+strconv.Itoa(port)); err != nil {
		return nil, fmt.Errorf("adb reverse: %w: %s", err, strings.TrimSpace(out))
	}
	// Established connections survive the removal of the tunnel.
	defer adb(context.Background(), o, "reverse", "--remove", socket) //nolint:errcheck

	args := []string{
		"-s", o.Serial, "shell",
		"CLASSPATH=" + devicePath, "app_process", "/", "com.genymobile.scrcpy.Server", serverVersion,
		fmt.Sprintf("scid=%08x", scid),
		"log_level=info",
		"audio=false",
		"control=true",
		"video_codec=h264",
		"clipboard_autosync=false",
		"cleanup=true",
	}
	if o.MaxSize > 0 {
		args = append(args, "max_size="+strconv.Itoa(o.MaxSize))
	}
	if o.MaxFPS > 0 {
		args = append(args, "max_fps="+strconv.Itoa(o.MaxFPS))
	}
	if o.BitRate > 0 {
		args = append(args, "video_bit_rate="+strconv.Itoa(o.BitRate))
	}
	s := &Session{
		cmd:    exec.Command(o.AdbPath, args...),
		output: &tailBuffer{max: 8 << 10},
		exited: make(chan struct{}),
		ctl:    make(chan []byte, controlBacklog),
	}
	s.cmd.Stdout = s.output
	s.cmd.Stderr = s.output
	if err := s.cmd.Start(); err != nil {
		return nil, fmt.Errorf("start scrcpy server: %w", err)
	}
	go func() {
		_ = s.cmd.Wait()
		close(s.exited)
	}()

	// Abort the accepts if the server dies or startup times out.
	connected := make(chan struct{})
	go func() {
		select {
		case <-s.exited:
		case <-ctx.Done():
		case <-connected:
			return
		}
		ln.Close()
	}()
	// The server connects the video socket first, then the control one.
	s.video, err = ln.Accept()
	if err == nil {
		s.control, err = ln.Accept()
	}
	close(connected)
	if err != nil {
		s.Close()
		return nil, s.startError(ctx, err)
	}

	// The device name, then the codec id, come first on the video socket.
	_ = s.video.SetReadDeadline(time.Now().Add(startTimeout))
	if s.DeviceName, err = readDeviceName(s.video); err == nil {
		err = readCodec(s.video)
	}
	_ = s.video.SetReadDeadline(time.Time{})
	if err != nil {
		s.Close()
		return nil, s.startError(ctx, err)
	}

	go s.writeControl()
	// Device messages (clipboard...) aren't used, but must be read.
	go io.Copy(io.Discard, s.control) //nolint:errcheck
	return s, nil
}

// startError explains a startup failure, preferring the server's output.
func (s *Session) startError(ctx context.Context, err error) error {
	select {
	case <-s.exited:
	case <-time.After(time.Second):
	}
	if out := strings.TrimSpace(s.output.String()); out != "" {
		return fmt.Errorf("scrcpy server failed: %s", lastLines(out, 3))
	}
	if ctx.Err() != nil {
		return errors.New("scrcpy server didn't connect in time")
	}
	return fmt.Errorf("scrcpy server: %w", err)
}

// ReadPacket returns the next video packet; Data is only valid until the
// next call. It fails once the session is closed or the device is gone.
func (s *Session) ReadPacket() (Packet, error) {
	p, buf, err := readPacket(s.video, s.buf)
	s.buf = buf
	return p, err
}

// Done is closed when the server process exits.
func (s *Session) Done() <-chan struct{} { return s.exited }

// Output returns the last lines printed by the server.
func (s *Session) Output() string { return s.output.String() }

// Touch injects a touchscreen event at (x, y) in the coordinates of a
// frame of width x height (the size of the last video frame).
func (s *Session) Touch(action uint8, x, y, width, height int) {
	pressure := float32(1)
	if action == MotionActionUp {
		pressure = 0
	}
	s.send(touchMsg(action, PointerFinger, position(x, y, width, height), pressure, 0, 0))
}

// Scroll injects a scroll at (x, y); h and v are in scroll "clicks"
// (positive v scrolls up).
func (s *Session) Scroll(x, y, width, height int, h, v float32) {
	s.send(scrollMsg(position(x, y, width, height), h, v, 0))
}

// Key injects a key press (down and up) of an Android key code.
func (s *Session) Key(keycode uint32) {
	s.send(keycodeMsg(KeyActionDown, keycode, 0, 0))
	s.send(keycodeMsg(KeyActionUp, keycode, 0, 0))
}

// Text injects text as key events.
func (s *Session) Text(text string) {
	if text != "" {
		s.send(textMsg(text))
	}
}

// Back presses BACK, or turns the screen on if it's off.
func (s *Session) Back() {
	s.send(backOrScreenOnMsg(KeyActionDown))
	s.send(backOrScreenOnMsg(KeyActionUp))
}

func position(x, y, width, height int) Position {
	return Position{X: int32(x), Y: int32(y), Width: uint16(width), Height: uint16(height)}
}

// send queues a control message without blocking the caller (UI events).
// If the device doesn't keep up, events are dropped.
func (s *Session) send(msg []byte) {
	s.ctlMu.Lock()
	defer s.ctlMu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ctl <- msg:
	default:
	}
}

func (s *Session) writeControl() {
	for msg := range s.ctl {
		if _, err := s.control.Write(msg); err != nil {
			return
		}
	}
}

// Close stops the session: closing the sockets makes the server exit
// (and clean up); the adb process is killed if it lingers.
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		s.ctlMu.Lock()
		s.closed = true
		close(s.ctl)
		s.ctlMu.Unlock()
		if s.video != nil {
			s.video.Close()
		}
		if s.control != nil {
			s.control.Close()
		}
		select {
		case <-s.exited:
		case <-time.After(2 * time.Second):
			_ = s.cmd.Process.Kill()
			<-s.exited
		}
	})
}

var (
	jarOnce sync.Once
	jarPath string
	jarErr  error
)

// push copies the embedded server to the device.
func push(ctx context.Context, o Options) error {
	jarOnce.Do(func() {
		jarPath = filepath.Join(os.TempDir(), "android-companion-scrcpy-server-v"+serverVersion+".jar")
		jarErr = os.WriteFile(jarPath, serverJar, 0o644)
	})
	if jarErr != nil {
		return jarErr
	}
	if out, err := adb(ctx, o, "push", jarPath, devicePath); err != nil {
		return fmt.Errorf("push scrcpy server: %w: %s", err, lastLines(strings.TrimSpace(out), 1))
	}
	return nil
}

func adb(ctx context.Context, o Options, args ...string) (string, error) {
	args = append([]string{"-s", o.Serial}, args...)
	out, err := exec.CommandContext(ctx, o.AdbPath, args...).CombinedOutput()
	return string(out), err
}

func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " ")
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf.Write(p)
	if extra := t.buf.Len() - t.max; extra > 0 {
		t.buf.Next(extra)
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.buf.String()
}
