// Package video decodes the mirrored device screen and hands frames to the
// UI.
package video

import (
	"errors"
	"image"
	"sync"
)

// ErrUnavailable is returned by NewH264 in builds without FFmpeg
// (`-tags nomirror`, or cgo disabled).
var ErrUnavailable = errors.New("this build has no video decoder (built without FFmpeg)")

// Sink receives decoded frames: Buffer provides the destination image for
// a frame of w x h (or nil to drop it), and Publish hands it back filled.
type Sink interface {
	Buffer(w, h int) *image.RGBA
	Publish(img *image.RGBA)
}

// FrameBuffer is a triple buffer between the decoder goroutine and the UI:
// the decoder always has a free image to write into, and the UI takes the
// latest complete frame, skipping older ones. Images are reused, so the
// one being displayed is never written.
type FrameBuffer struct {
	mu        sync.Mutex
	imgs      [3]*image.RGBA
	displayed int // index the UI shows (-1: none)
	pending   int // latest published frame not yet taken (-1: none)
	writing   int // index handed out by Buffer (-1: none)
}

// NewFrameBuffer creates an empty frame buffer.
func NewFrameBuffer() *FrameBuffer {
	return &FrameBuffer{displayed: -1, pending: -1, writing: -1}
}

// Buffer returns an image of w x h that is neither displayed nor pending.
func (fb *FrameBuffer) Buffer(w, h int) *image.RGBA {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	for i := range fb.imgs {
		if i == fb.displayed || i == fb.pending {
			continue
		}
		img := fb.imgs[i]
		if img == nil || img.Rect.Dx() != w || img.Rect.Dy() != h {
			img = image.NewRGBA(image.Rect(0, 0, w, h))
			fb.imgs[i] = img
		}
		fb.writing = i
		return img
	}
	return nil // unreachable: 3 buffers, at most 2 in use
}

// Publish marks the image from the last Buffer call as the latest frame.
func (fb *FrameBuffer) Publish(img *image.RGBA) {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	if fb.writing >= 0 && fb.imgs[fb.writing] == img {
		fb.pending = fb.writing
	}
	fb.writing = -1
}

// Take returns the latest frame if there's a new one, and marks it as
// displayed: it won't be written until the next Take. Call it from the UI
// goroutine, right before showing the image.
func (fb *FrameBuffer) Take() *image.RGBA {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	if fb.pending < 0 {
		return nil
	}
	fb.displayed, fb.pending = fb.pending, -1
	return fb.imgs[fb.displayed]
}
