//go:build !cgo || nomirror

package video

// Available reports whether this build can decode video.
const Available = false

// Decoder is a placeholder in builds without FFmpeg.
type Decoder struct{}

// NewH264 always fails in builds without FFmpeg.
func NewH264() (*Decoder, error) { return nil, ErrUnavailable }

// Decode does nothing.
func (d *Decoder) Decode([]byte, Sink) error { return ErrUnavailable }

// Close does nothing.
func (d *Decoder) Close() {}
