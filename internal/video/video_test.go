package video

import (
	"image"
	"os"
	"testing"
)

// recordSink collects published frames.
type recordSink struct {
	fb     *FrameBuffer
	frames []*image.RGBA
}

func (s *recordSink) Buffer(w, h int) *image.RGBA { return s.fb.Buffer(w, h) }
func (s *recordSink) Publish(img *image.RGBA) {
	s.fb.Publish(img)
	s.frames = append(s.frames, img)
}

func TestDecodeH264(t *testing.T) {
	if !Available {
		t.Skip("built without FFmpeg")
	}
	data, err := os.ReadFile("testdata/red_64x48.h264")
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewH264()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	sink := &recordSink{fb: NewFrameBuffer()}
	if err := d.Decode(data, sink); err != nil {
		t.Fatal(err)
	}
	if len(sink.frames) != 1 {
		t.Fatalf("decoded %d frames, want 1 (low delay)", len(sink.frames))
	}
	img := sink.frames[0]
	if img.Rect.Dx() != 64 || img.Rect.Dy() != 48 {
		t.Fatalf("frame size = %v, want 64x48", img.Rect)
	}
	c := img.RGBAAt(32, 24)
	if c.R < 230 || c.G > 30 || c.B > 30 || c.A != 255 {
		t.Fatalf("center pixel = %+v, want red", c)
	}
	if got := sink.fb.Take(); got != img {
		t.Fatal("Take didn't return the decoded frame")
	}
}

func TestDecodeGarbage(t *testing.T) {
	if !Available {
		t.Skip("built without FFmpeg")
	}
	d, err := NewH264()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	// Garbage must not crash; it may or may not report an error.
	_ = d.Decode([]byte{0, 0, 0, 1, 0x65, 0xff, 0xff, 0xff}, &recordSink{fb: NewFrameBuffer()})
}

func TestFrameBuffer(t *testing.T) {
	fb := NewFrameBuffer()
	if fb.Take() != nil {
		t.Fatal("Take on empty buffer")
	}

	a := fb.Buffer(4, 2)
	fb.Publish(a)
	if got := fb.Take(); got != a {
		t.Fatal("Take didn't return the published frame")
	}
	if fb.Take() != nil {
		t.Fatal("Take returned the same frame twice")
	}

	// The displayed image (a) is never handed out for writing.
	b := fb.Buffer(4, 2)
	fb.Publish(b)
	c := fb.Buffer(4, 2) // b is pending, a displayed
	if b == a || c == a || c == b {
		t.Fatal("Buffer returned an image in use")
	}
	fb.Publish(c)
	// The newest frame wins; b is free again.
	if got := fb.Take(); got != c {
		t.Fatal("Take didn't return the newest frame")
	}
	if d := fb.Buffer(4, 2); d == c {
		t.Fatal("Buffer returned the displayed image")
	}

	// A new size reallocates.
	if img := fb.Buffer(8, 8); img.Rect.Dx() != 8 {
		t.Fatalf("size = %v, want 8x8", img.Rect)
	}
}
