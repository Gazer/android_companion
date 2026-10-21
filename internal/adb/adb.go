// Package adb wraps the adb commands used to find devices and connect to
// them, whatever the transport (USB, Wi-Fi or a running emulator).
package adb

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// StateReady is the adb state of a device that accepts commands.
const StateReady = "device"

// commandTimeout bounds every adb call made by this package.
const commandTimeout = 15 * time.Second

// Device is one entry of `adb devices -l`.
type Device struct {
	Serial string
	// State is StateReady when usable; otherwise "unauthorized", "offline",
	// "no permissions", "authorizing", ...
	State       string
	Model       string // e.g. "Pixel_5"
	Product     string
	USB         string // USB port (e.g. "1-1"); empty for network devices
	TransportID string
}

// Ready reports whether the device accepts commands.
func (d Device) Ready() bool { return d.State == StateReady }

// IsEmulator reports whether the device is a running emulator.
func (d Device) IsEmulator() bool { return strings.HasPrefix(d.Serial, "emulator-") }

// IsNetwork reports whether the device is connected over TCP/IP: wireless
// debugging (mDNS serial) or `adb connect host:port`.
func (d Device) IsNetwork() bool {
	if strings.Contains(d.Serial, "._adb-tls-connect._tcp") {
		return true
	}
	host, port, err := net.SplitHostPort(d.Serial)
	return err == nil && host != "" && isPort(port)
}

// Name is a human-friendly device name ("Pixel 5"), or the serial.
func (d Device) Name() string {
	if d.Model != "" {
		return strings.ReplaceAll(d.Model, "_", " ")
	}
	return d.Serial
}

// StateHint explains a non-ready state to the user.
func (d Device) StateHint() string {
	switch d.State {
	case StateReady:
		return ""
	case "unauthorized":
		return "Unauthorized: allow USB debugging on the device"
	case "offline":
		return "Offline: reconnect the device or restart adb"
	case "no permissions":
		return "No permissions: check the udev rules for the device"
	case "authorizing", "connecting":
		return "Connecting..."
	default:
		return "Not available (" + d.State + ")"
	}
}

// ParseDevices parses the output of `adb devices -l`.
func ParseDevices(out string) []Device {
	var devs []Device
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "*") || strings.HasPrefix(line, "List of devices") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		d := Device{Serial: fields[0], State: fields[1]}
		rest := fields[2:]
		// "no permissions (missing udev rules? ...); see [...]" is the only
		// multi-word state.
		if d.State == "no" && len(rest) > 0 && strings.HasPrefix(rest[0], "permissions") {
			d.State = "no permissions"
		}
		for _, f := range rest {
			k, v, ok := strings.Cut(f, ":")
			if !ok {
				continue
			}
			switch k {
			case "usb":
				d.USB = v
			case "product":
				d.Product = v
			case "model":
				d.Model = v
			case "transport_id":
				d.TransportID = v
			}
		}
		devs = append(devs, d)
	}
	return devs
}

// ListDevices returns the devices known to the adb server.
func ListDevices(ctx context.Context, adbPath string) ([]Device, error) {
	out, err := run(ctx, adbPath, "devices", "-l")
	if err != nil {
		return nil, fmt.Errorf("adb devices: %w", commandError(err, out))
	}
	return ParseDevices(out), nil
}

// GetState returns the adb state of a device ("device" when ready).
func GetState(ctx context.Context, adbPath, serial string) (string, error) {
	out, err := run(ctx, adbPath, "-s", serial, "get-state")
	if err != nil {
		return "", commandError(err, out)
	}
	return strings.TrimSpace(out), nil
}

// Info describes a device's hardware and Android version.
type Info struct {
	Manufacturer string
	Model        string
	Release      string // Android version, e.g. "14"
	SDK          string // API level, e.g. "34"
}

// Name returns "Manufacturer Model", without repeating the manufacturer
// when the model already starts with it.
func (i Info) Name() string {
	switch {
	case i.Model == "":
		return i.Manufacturer
	case i.Manufacturer == "" || strings.HasPrefix(strings.ToLower(i.Model), strings.ToLower(i.Manufacturer)):
		return i.Model
	default:
		return i.Manufacturer + " " + i.Model
	}
}

// Version returns e.g. "Android 14 (API 34)".
func (i Info) Version() string {
	switch {
	case i.Release == "" && i.SDK == "":
		return ""
	case i.SDK == "":
		return "Android " + i.Release
	default:
		return "Android " + i.Release + " (API " + i.SDK + ")"
	}
}

// GetInfo reads the device's model and Android version. It fails if the
// device can't be reached (not found, unauthorized, offline).
func GetInfo(ctx context.Context, adbPath, serial string) (Info, error) {
	out, err := run(ctx, adbPath, "-s", serial, "shell",
		"getprop ro.product.manufacturer; getprop ro.product.model; "+
			"getprop ro.build.version.release; getprop ro.build.version.sdk")
	if err != nil {
		return Info{}, commandError(err, out)
	}
	lines := strings.Split(strings.ReplaceAll(out, "\r", ""), "\n")
	for len(lines) < 4 {
		lines = append(lines, "")
	}
	return Info{
		Manufacturer: strings.TrimSpace(lines[0]),
		Model:        strings.TrimSpace(lines[1]),
		Release:      strings.TrimSpace(lines[2]),
		SDK:          strings.TrimSpace(lines[3]),
	}, nil
}

// EmulatorAVDName returns the AVD name of a running emulator
// (`adb emu avd name`).
func EmulatorAVDName(ctx context.Context, adbPath, serial string) (string, error) {
	out, err := run(ctx, adbPath, "-s", serial, "emu", "avd", "name")
	if err != nil {
		return "", commandError(err, out)
	}
	name, _, _ := strings.Cut(strings.ReplaceAll(out, "\r", ""), "\n")
	name = strings.TrimSpace(name)
	if name == "" || name == "OK" {
		return "", errors.New("no AVD name")
	}
	return name, nil
}

// Pair pairs with a device using wireless debugging's pairing code
// (Android 11+). hostPort is the address shown in the device's pairing
// dialog. Once paired, adb connects to the device automatically (mDNS).
func Pair(ctx context.Context, adbPath, hostPort, code string) error {
	out, err := run(ctx, adbPath, "pair", hostPort, code)
	if strings.Contains(out, "Successfully paired") {
		return nil
	}
	return commandError(errOrFailed(err), out)
}

// Connect connects to a device over TCP/IP (`adb connect`).
func Connect(ctx context.Context, adbPath, hostPort string) error {
	out, err := run(ctx, adbPath, "connect", hostPort)
	msg := strings.TrimSpace(out)
	if strings.HasPrefix(msg, "connected to") || strings.HasPrefix(msg, "already connected to") {
		return nil
	}
	return commandError(errOrFailed(err), out)
}

// NormalizeAddress validates a "host:port" address. If the port is missing
// and defaultPort isn't empty, it's added.
func NormalizeAddress(addr, defaultPort string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", errors.New("enter the IP address and port")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		if defaultPort == "" || strings.Count(addr, ":") > 1 {
			return "", fmt.Errorf("%q is not a valid IP address and port", addr)
		}
		host, port = addr, defaultPort
	}
	if host == "" || !isPort(port) {
		return "", fmt.Errorf("%q is not a valid IP address and port", addr)
	}
	return net.JoinHostPort(host, port), nil
}

func isPort(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n > 0 && n < 65536
}

func run(ctx context.Context, adbPath string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, adbPath, args...).CombinedOutput()
	return string(out), err
}

var errFailed = errors.New("failed")

func errOrFailed(err error) error {
	if err != nil {
		return err
	}
	return errFailed
}

// commandError prefers adb's own message (first meaningful output line)
// over the exit status.
func commandError(err error, out string) error {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "*") {
			return errors.New(strings.TrimPrefix(line, "adb: "))
		}
	}
	return err
}
