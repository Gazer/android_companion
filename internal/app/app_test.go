package app

import (
	"context"
	"errors"
	"testing"

	"github.com/rmarkiewicz/android-companion/internal/adb"
	"github.com/rmarkiewicz/android-companion/internal/avd"
)

func TestDeviceTargets(t *testing.T) {
	s := &session{mgr: &avd.Manager{AdbBin: "/nonexistent/adb"}}
	devs := []adb.Device{
		{Serial: "11041FDD4002MQ", State: "device", Model: "Pixel_5", USB: "1-1"},
		{Serial: "192.168.1.20:5555", State: "device", Model: "Pixel_7"},
		{Serial: "R58M123ABC", State: "unauthorized"},
		{Serial: "emulator-5554", State: "device", Model: "sdk_gphone64_arm64"},
	}
	// The emulator's AVD name is cached, so adb isn't called.
	names := map[string]string{"emulator-5554": "Pixel_8_API_35"}
	targets, note := s.deviceTargets(context.Background(), devs, nil, names)
	if note != "" || len(targets) != 4 {
		t.Fatalf("targets = %+v, note = %q", targets, note)
	}

	want := []struct {
		title, detail string
		ready         bool
	}{
		{"Pixel 5", "USB · 11041FDD4002MQ", true},
		{"Pixel 7", "Wi-Fi · 192.168.1.20:5555", true},
		{"R58M123ABC", "Unauthorized: allow USB debugging on the device", false},
		{"Pixel 8 API 35", "Running emulator · emulator-5554 · logs only", true},
	}
	for i, w := range want {
		got := targets[i]
		if got.Title != w.title || got.Detail != w.detail || got.Ready != w.ready || got.ID != devs[i].Serial {
			t.Errorf("target %d = %+v, want %+v", i, got, w)
		}
	}

	if targets, note := s.deviceTargets(context.Background(), nil, nil, names); len(targets) != 0 || note == "" {
		t.Errorf("no devices: targets = %+v, note = %q", targets, note)
	}
	if _, note := s.deviceTargets(context.Background(), nil, errors.New("adb devices: boom"), names); note != "adb devices: boom" {
		t.Errorf("error note = %q", note)
	}
}

func TestPairValidation(t *testing.T) {
	s := &session{mgr: &avd.Manager{AdbBin: "/nonexistent/adb"}, pollNow: make(chan struct{}, 1)}
	ctx := context.Background()
	if err := s.pair(ctx, "192.168.1.20", "123456"); err == nil {
		t.Error("pairing without a port must fail")
	}
	if err := s.pair(ctx, "192.168.1.20:37000", "12ab56"); err == nil {
		t.Error("pairing with a non-numeric code must fail")
	}
	if err := s.connect(ctx, "not an address:x"); err == nil {
		t.Error("connect with a bad address must fail")
	}
}
