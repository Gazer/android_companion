package adb

import (
	"reflect"
	"testing"
)

func TestParseDevices(t *testing.T) {
	out := `* daemon not running; starting now at tcp:5037
* daemon started successfully
List of devices attached
11041FDD4002MQ         device usb:1-1 product:redfin model:Pixel_5 device:redfin transport_id:41
emulator-5554          device product:sdk_gphone64_arm64 model:sdk_gphone64_arm64 device:emu64a transport_id:2
adb-11041FDD4002MQ-Ab12Cd._adb-tls-connect._tcp device product:redfin model:Pixel_5 device:redfin transport_id:5
192.168.1.20:5555      offline transport_id:6
R58M123ABC             unauthorized usb:2-1 transport_id:7
0123456789ABCDEF       no permissions (missing udev rules? user is in the plugdev group); see [http://developer.android.com/tools/device.html] usb:3-1 transport_id:8

`
	got := ParseDevices(out)
	want := []Device{
		{Serial: "11041FDD4002MQ", State: "device", Model: "Pixel_5", Product: "redfin", USB: "1-1", TransportID: "41"},
		{Serial: "emulator-5554", State: "device", Model: "sdk_gphone64_arm64", Product: "sdk_gphone64_arm64", TransportID: "2"},
		{Serial: "adb-11041FDD4002MQ-Ab12Cd._adb-tls-connect._tcp", State: "device", Model: "Pixel_5", Product: "redfin", TransportID: "5"},
		{Serial: "192.168.1.20:5555", State: "offline", TransportID: "6"},
		{Serial: "R58M123ABC", State: "unauthorized", USB: "2-1", TransportID: "7"},
		{Serial: "0123456789ABCDEF", State: "no permissions", USB: "3-1", TransportID: "8"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseDevices mismatch\n got: %+v\nwant: %+v", got, want)
	}

	if !got[0].Ready() || got[3].Ready() || got[4].Ready() {
		t.Error("Ready() wrong")
	}
	if got[0].IsNetwork() || got[1].IsNetwork() || !got[2].IsNetwork() || !got[3].IsNetwork() {
		t.Error("IsNetwork() wrong")
	}
	if !got[1].IsEmulator() || got[0].IsEmulator() {
		t.Error("IsEmulator() wrong")
	}
	if got[0].Name() != "Pixel 5" || got[3].Name() != "192.168.1.20:5555" {
		t.Errorf("Name() = %q, %q", got[0].Name(), got[3].Name())
	}
	if got[4].StateHint() == "" || got[0].StateHint() != "" {
		t.Error("StateHint() wrong")
	}
}

func TestInfo(t *testing.T) {
	tests := []struct {
		info          Info
		name, version string
	}{
		{Info{"Google", "Pixel 5", "14", "34"}, "Google Pixel 5", "Android 14 (API 34)"},
		{Info{"samsung", "SM-G991B", "13", "33"}, "samsung SM-G991B", "Android 13 (API 33)"},
		{Info{"Xiaomi", "Xiaomi 13", "14", ""}, "Xiaomi 13", "Android 14"},
		{Info{}, "", ""},
	}
	for _, tt := range tests {
		if got := tt.info.Name(); got != tt.name {
			t.Errorf("Name(%+v) = %q, want %q", tt.info, got, tt.name)
		}
		if got := tt.info.Version(); got != tt.version {
			t.Errorf("Version(%+v) = %q, want %q", tt.info, got, tt.version)
		}
	}
}

func TestNormalizeAddress(t *testing.T) {
	tests := []struct {
		in, def, want string
		ok            bool
	}{
		{"192.168.1.20:37123", "", "192.168.1.20:37123", true},
		{" 192.168.1.20:37123 ", "", "192.168.1.20:37123", true},
		{"192.168.1.20", "5555", "192.168.1.20:5555", true},
		{"192.168.1.20", "", "", false},
		{"192.168.1.20:99999", "", "", false},
		{":5555", "", "", false},
		{"", "5555", "", false},
		{"[fe80::1]:5555", "", "[fe80::1]:5555", true},
	}
	for _, tt := range tests {
		got, err := NormalizeAddress(tt.in, tt.def)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("NormalizeAddress(%q, %q) = %q, %v; want %q, ok=%v", tt.in, tt.def, got, err, tt.want, tt.ok)
		}
	}
}

func TestCommandError(t *testing.T) {
	err := commandError(errFailed, "adb: device unauthorized.\nThis adb server's $ADB_VENDOR_KEYS is not set\n")
	if err.Error() != "device unauthorized." {
		t.Errorf("commandError = %q", err)
	}
	if err := commandError(errFailed, ""); err != errFailed {
		t.Errorf("commandError with no output = %v, want errFailed", err)
	}
}
