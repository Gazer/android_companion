package ui

import (
	"fmt"
	"image"
	"reflect"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
)

type recordInput struct{ events []string }

func (r *recordInput) Pointer(a PointerAction, x, y, w, h int) {
	r.events = append(r.events, fmt.Sprintf("pointer %d %d,%d %dx%d", a, x, y, w, h))
}
func (r *recordInput) Scroll(x, y, w, h int, dx, dy float32) {
	r.events = append(r.events, fmt.Sprintf("scroll %d,%d %.1f %.1f", x, y, dx, dy))
}
func (r *recordInput) Key(k ScreenKey) { r.events = append(r.events, fmt.Sprintf("key %d", k)) }
func (r *recordInput) Text(s string)   { r.events = append(r.events, "text "+s) }

func newTestScreen(t *testing.T) (*ScreenWidget, *recordInput, fyne.Window) {
	test.NewTempApp(t)
	s := NewScreenWidget()
	in := &recordInput{}
	s.SetInput(in)
	w := test.NewWindow(s)
	t.Cleanup(w.Close)
	w.Resize(fyne.NewSize(400, 800))
	s.Resize(fyne.NewSize(400, 800))
	// A 100x100 frame shown contained in 400x800: 4 units per pixel,
	// centered vertically (offset 200).
	s.SetImage(image.NewRGBA(image.Rect(0, 0, 100, 100)))
	return s, in, w
}

func mouse(x, y float32) *desktop.MouseEvent {
	return &desktop.MouseEvent{
		PointEvent: fyne.PointEvent{Position: fyne.NewPos(x, y)},
		Button:     desktop.MouseButtonPrimary,
	}
}

func TestScreenPointer(t *testing.T) {
	s, in, _ := newTestScreen(t)

	s.MouseDown(mouse(200, 400)) // center
	s.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(240, 400)}})
	s.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(1000, 100)}}) // outside: clamped
	s.MouseUp(mouse(1000, 100))
	s.DragEnd() // after MouseUp: no second "up"

	want := []string{
		"pointer 0 50,50 100x100",
		"pointer 1 60,50 100x100",
		"pointer 1 99,0 100x100",
		"pointer 2 99,0 100x100",
	}
	if !reflect.DeepEqual(in.events, want) {
		t.Fatalf("events:\n got %q\nwant %q", in.events, want)
	}
}

func TestScreenPointerOutsideFrame(t *testing.T) {
	s, in, _ := newTestScreen(t)
	s.MouseDown(mouse(200, 100)) // above the frame (letterbox)
	s.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(200, 400)}})
	s.MouseUp(mouse(200, 400))
	if len(in.events) != 0 {
		t.Fatalf("events for a press outside the frame: %q", in.events)
	}

	// Secondary button is ignored.
	e := mouse(200, 400)
	e.Button = desktop.MouseButtonSecondary
	s.MouseDown(e)
	if len(in.events) != 0 {
		t.Fatalf("events for the secondary button: %q", in.events)
	}
}

func TestScreenScrollAndKeys(t *testing.T) {
	s, in, _ := newTestScreen(t)
	s.Scrolled(&fyne.ScrollEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(200, 400)}, Scrolled: fyne.NewDelta(0, 20)})
	s.TypedRune('a')
	s.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	s.TypedKey(&fyne.KeyEvent{Name: fyne.KeyA}) // text keys come as TypedRune
	s.TypedKey(&fyne.KeyEvent{Name: fyne.KeyBackspace})

	want := []string{
		"scroll 50,50 0.0 2.0",
		"text a",
		fmt.Sprintf("key %d", KeyEnter),
		fmt.Sprintf("key %d", KeyBackspace),
	}
	if !reflect.DeepEqual(in.events, want) {
		t.Fatalf("events:\n got %q\nwant %q", in.events, want)
	}
	if !s.AcceptsTab() {
		t.Error("Tab must go to the device")
	}
}

func TestScreenNoFrame(t *testing.T) {
	s, in, _ := newTestScreen(t)
	s.Clear()
	s.MouseDown(mouse(200, 400))
	s.Scrolled(&fyne.ScrollEvent{PointEvent: fyne.PointEvent{Position: fyne.NewPos(200, 400)}})
	if len(in.events) != 0 {
		t.Fatalf("events without a frame: %q", in.events)
	}
}
