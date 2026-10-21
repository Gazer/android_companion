package scrcpy

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"unicode/utf8"
)

// Video codec ids sent by the server before the stream.
const (
	codecH264 = 0x68323634 // "h264"
	// The server sends 0 when it disables the stream, 1 on configuration error.
	codecDisabled = 0
	codecError    = 1
)

// deviceNameLength is the size of the device name sent first on the video
// socket.
const deviceNameLength = 64

// Frame header flags (most significant bits of the 64-bit PTS field).
const (
	flagSession  = uint64(1) << 63
	flagConfig   = uint64(1) << 62
	flagKeyFrame = uint64(1) << 61
	ptsMask      = flagKeyFrame - 1
)

// maxPacketSize guards against a corrupt stream.
const maxPacketSize = 64 << 20

// Packet is one message of the video stream.
type Packet struct {
	// Session is set for a session packet: a new capture session started
	// (e.g. the device rotated) with frames of Width x Height.
	Session       bool
	Width, Height int

	// Media packets: Config holds codec configuration (SPS/PPS) to be
	// prepended to the next frame; KeyFrame marks an IDR frame.
	Config   bool
	KeyFrame bool
	PTS      int64
	Data     []byte
}

// readDeviceName reads the 64-byte NUL-padded device name.
func readDeviceName(r io.Reader) (string, error) {
	var buf [deviceNameLength]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return "", err
	}
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return string(buf[:n]), nil
}

// readCodec reads the codec id and checks it's H.264.
func readCodec(r io.Reader) error {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return err
	}
	switch id := binary.BigEndian.Uint32(buf[:]); id {
	case codecH264:
		return nil
	case codecDisabled:
		return errors.New("the device disabled the video stream")
	case codecError:
		return errors.New("the device could not configure the video stream")
	default:
		return fmt.Errorf("unexpected video codec 0x%08x", id)
	}
}

// readPacket reads the next video packet. buf is reused for the payload
// when large enough; the returned Data is only valid until the next call.
func readPacket(r io.Reader, buf []byte) (Packet, []byte, error) {
	var header [12]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Packet{}, buf, err
	}
	first := binary.BigEndian.Uint64(header[:8])
	if first&flagSession != 0 {
		return Packet{
			Session: true,
			Width:   int(binary.BigEndian.Uint32(header[4:8])),
			Height:  int(binary.BigEndian.Uint32(header[8:12])),
		}, buf, nil
	}

	size := binary.BigEndian.Uint32(header[8:12])
	if size > maxPacketSize {
		return Packet{}, buf, fmt.Errorf("video packet too large (%d bytes)", size)
	}
	if cap(buf) < int(size) {
		buf = make([]byte, size)
	}
	buf = buf[:size]
	if _, err := io.ReadFull(r, buf); err != nil {
		return Packet{}, buf, err
	}
	return Packet{
		Config:   first&flagConfig != 0,
		KeyFrame: first&flagKeyFrame != 0,
		PTS:      int64(first & ptsMask),
		Data:     buf,
	}, buf, nil
}

// Control message types (scrcpy 4.0, app/src/control_msg.h).
const (
	msgInjectKeycode    = 0
	msgInjectText       = 1
	msgInjectTouch      = 2
	msgInjectScroll     = 3
	msgBackOrScreenOn   = 4
	maxInjectTextLength = 300
)

// Android key event actions.
const (
	KeyActionDown = 0
	KeyActionUp   = 1
)

// Android motion event actions.
const (
	MotionActionDown = 0
	MotionActionUp   = 1
	MotionActionMove = 2
)

// PointerFinger is scrcpy's generic finger pointer id: events are injected
// as touchscreen events.
const PointerFinger = ^uint64(1) // -2

// Android key codes used by the app.
const (
	KeycodeHome       = 3
	KeycodeBack       = 4
	KeycodeDPadUp     = 19
	KeycodeDPadDown   = 20
	KeycodeDPadLeft   = 21
	KeycodeDPadRight  = 22
	KeycodeTab        = 61
	KeycodeEnter      = 66
	KeycodeDel        = 67 // backspace
	KeycodePageUp     = 92
	KeycodePageDown   = 93
	KeycodeForwardDel = 112
	KeycodeMoveHome   = 122
	KeycodeMoveEnd    = 123
	KeycodeAppSwitch  = 187
)

// Position is a point in video frame coordinates, with the frame size the
// point refers to (the server ignores events for a stale size).
type Position struct {
	X, Y          int32
	Width, Height uint16
}

func putPosition(b []byte, p Position) {
	binary.BigEndian.PutUint32(b[0:], uint32(p.X))
	binary.BigEndian.PutUint32(b[4:], uint32(p.Y))
	binary.BigEndian.PutUint16(b[8:], p.Width)
	binary.BigEndian.PutUint16(b[10:], p.Height)
}

// keycodeMsg serializes INJECT_KEYCODE.
func keycodeMsg(action uint8, keycode, repeat, metaState uint32) []byte {
	b := make([]byte, 14)
	b[0] = msgInjectKeycode
	b[1] = action
	binary.BigEndian.PutUint32(b[2:], keycode)
	binary.BigEndian.PutUint32(b[6:], repeat)
	binary.BigEndian.PutUint32(b[10:], metaState)
	return b
}

// textMsg serializes INJECT_TEXT, truncating to the server's limit
// without splitting UTF-8 sequences.
func textMsg(text string) []byte {
	if len(text) > maxInjectTextLength {
		n := maxInjectTextLength
		for n > 0 && !utf8.RuneStart(text[n]) {
			n--
		}
		text = text[:n]
	}
	b := make([]byte, 5+len(text))
	b[0] = msgInjectText
	binary.BigEndian.PutUint32(b[1:], uint32(len(text)))
	copy(b[5:], text)
	return b
}

// touchMsg serializes INJECT_TOUCH_EVENT. pressure is in [0, 1].
func touchMsg(action uint8, pointerID uint64, pos Position, pressure float32, actionButton, buttons uint32) []byte {
	b := make([]byte, 32)
	b[0] = msgInjectTouch
	b[1] = action
	binary.BigEndian.PutUint64(b[2:], pointerID)
	putPosition(b[10:], pos)
	binary.BigEndian.PutUint16(b[22:], floatToU16FP(pressure))
	binary.BigEndian.PutUint32(b[24:], actionButton)
	binary.BigEndian.PutUint32(b[28:], buttons)
	return b
}

// scrollMsg serializes INJECT_SCROLL_EVENT. hscroll and vscroll are in
// [-16, 16] (scroll "clicks").
func scrollMsg(pos Position, hscroll, vscroll float32, buttons uint32) []byte {
	b := make([]byte, 21)
	b[0] = msgInjectScroll
	putPosition(b[1:], pos)
	binary.BigEndian.PutUint16(b[13:], uint16(floatToI16FP(clamp(hscroll/16, -1, 1))))
	binary.BigEndian.PutUint16(b[15:], uint16(floatToI16FP(clamp(vscroll/16, -1, 1))))
	binary.BigEndian.PutUint32(b[17:], buttons)
	return b
}

// backOrScreenOnMsg serializes BACK_OR_SCREEN_ON: BACK, or turn the screen
// on if it's off.
func backOrScreenOnMsg(action uint8) []byte {
	return []byte{msgBackOrScreenOn, action}
}

// floatToU16FP converts [0, 1] to unsigned 16-bit fixed point (as scrcpy).
func floatToU16FP(f float32) uint16 {
	u := uint32(clamp(f, 0, 1) * 65536)
	if u > 0xffff {
		u = 0xffff
	}
	return uint16(u)
}

// floatToI16FP converts [-1, 1] to signed 16-bit fixed point (as scrcpy).
func floatToI16FP(f float32) int16 {
	i := int32(f * 32768)
	if i > math.MaxInt16 {
		i = math.MaxInt16
	}
	return int16(i)
}

func clamp(f, lo, hi float32) float32 {
	return max(lo, min(f, hi))
}
