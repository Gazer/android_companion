package ui

import (
	"testing"

	"fyne.io/fyne/v2/test"
)

func TestPickerSectionsAndSelection(t *testing.T) {
	test.NewTempApp(t)
	var opened []Target
	p := NewPickerPanel(func(t Target) { opened = append(opened, t) })

	usb := Target{Kind: TargetDevice, ID: "SERIAL1", Title: "Pixel 5", Detail: "USB · SERIAL1", Ready: true}
	locked := Target{Kind: TargetDevice, ID: "SERIAL2", Title: "SERIAL2", Detail: "Unauthorized: allow USB debugging on the device"}
	avd := Target{Kind: TargetAVD, ID: "Pixel_8", Title: "Pixel 8", Ready: true}

	p.SetDevices([]Target{usb, locked}, "")
	p.SetAVDs(nil, "No AVDs")

	// Connected devices header, 2 devices, Emulators header, note.
	if len(p.rows) != 5 {
		t.Fatalf("rows = %d, want 5: %+v", len(p.rows), p.rows)
	}
	if p.rows[0].header == "" || p.rows[3].header == "" || p.rows[4].note != "No AVDs" {
		t.Fatalf("unexpected rows: %+v", p.rows)
	}

	p.list.Select(0) // header: ignored
	p.list.Select(2) // not ready: explains why
	if len(opened) != 0 {
		t.Fatalf("opened %+v, want nothing", opened)
	}
	if p.status.Text == "" {
		t.Error("no status for a device that isn't ready")
	}

	p.list.Select(1)
	p.list.Select(1) // selecting again works (the list doesn't keep the selection)
	if len(opened) != 2 || opened[0] != usb {
		t.Fatalf("opened %+v, want USB device twice", opened)
	}

	p.SetAVDs([]Target{avd}, "")
	p.list.Select(4)
	if len(opened) != 3 || opened[2] != avd {
		t.Fatalf("opened %+v, want the AVD last", opened)
	}
}
