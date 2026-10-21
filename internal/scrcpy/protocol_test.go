package scrcpy

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"strings"
	"testing"
)

func TestEmbeddedServer(t *testing.T) {
	sum := sha256.Sum256(serverJar)
	const want = "84924bd564a1eb6089c872c7521f968058977f91f5ff02514a8c74aff3210f3a"
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("scrcpy-server sha256 = %s, want %s (serverVersion must match the jar)", got, want)
	}
}

// Expected bytes come from scrcpy v4.0 app/tests/test_control_msg_serialize.c.
func TestControlMessages(t *testing.T) {
	tests := []struct {
		name string
		got  []byte
		want []byte
	}{
		{
			"keycode",
			keycodeMsg(KeyActionUp, KeycodeEnter, 5, 0x41),
			[]byte{0x00, 0x01, 0x00, 0x00, 0x00, 0x42, 0x00, 0x00, 0x00, 0x05, 0x00, 0x00, 0x00, 0x41},
		},
		{
			"text",
			textMsg("hello, world!"),
			append([]byte{0x01, 0x00, 0x00, 0x00, 0x0d}, "hello, world!"...),
		},
		{
			"touch",
			touchMsg(MotionActionDown, 0x1234567887654321, Position{X: 100, Y: 200, Width: 1080, Height: 1920}, 1, 1, 1),
			[]byte{
				0x02, 0x00,
				0x12, 0x34, 0x56, 0x78, 0x87, 0x65, 0x43, 0x21,
				0x00, 0x00, 0x00, 0x64, 0x00, 0x00, 0x00, 0xc8,
				0x04, 0x38, 0x07, 0x80,
				0xff, 0xff,
				0x00, 0x00, 0x00, 0x01,
				0x00, 0x00, 0x00, 0x01,
			},
		},
		{
			"scroll",
			scrollMsg(Position{X: 260, Y: 1026, Width: 1080, Height: 1920}, 16, -16, 1),
			[]byte{
				0x03,
				0x00, 0x00, 0x01, 0x04, 0x00, 0x00, 0x04, 0x02,
				0x04, 0x38, 0x07, 0x80,
				0x7f, 0xff,
				0x80, 0x00,
				0x00, 0x00, 0x00, 0x01,
			},
		},
		{
			"back or screen on",
			backOrScreenOnMsg(KeyActionUp),
			[]byte{0x04, 0x01},
		},
	}
	for _, tt := range tests {
		if !bytes.Equal(tt.got, tt.want) {
			t.Errorf("%s:\n got % x\nwant % x", tt.name, tt.got, tt.want)
		}
	}
}

func TestTextMsgTruncation(t *testing.T) {
	long := strings.Repeat("a", maxInjectTextLength+50)
	if got := textMsg(long); len(got) != 5+maxInjectTextLength {
		t.Errorf("len = %d, want %d", len(got), 5+maxInjectTextLength)
	}
	// Truncation must not split a multi-byte character.
	utf := strings.Repeat("a", maxInjectTextLength-1) + "ñ"
	msg := textMsg(utf)
	if n := binary.BigEndian.Uint32(msg[1:5]); n != maxInjectTextLength-1 {
		t.Errorf("text length = %d, want %d", n, maxInjectTextLength-1)
	}
}

func TestFinger(t *testing.T) {
	if PointerFinger != 0xfffffffffffffffe {
		t.Fatalf("PointerFinger = %x, want -2", PointerFinger)
	}
}

func TestReadStream(t *testing.T) {
	var stream bytes.Buffer
	name := make([]byte, deviceNameLength)
	copy(name, "Pixel 5")
	stream.Write(name)
	binary.Write(&stream, binary.BigEndian, uint32(codecH264))
	// Session packet: 1080x2340.
	binary.Write(&stream, binary.BigEndian, uint32(0x80000000))
	binary.Write(&stream, binary.BigEndian, uint32(1080))
	binary.Write(&stream, binary.BigEndian, uint32(2340))
	// Config packet.
	binary.Write(&stream, binary.BigEndian, flagConfig)
	binary.Write(&stream, binary.BigEndian, uint32(3))
	stream.Write([]byte{1, 2, 3})
	// Key frame with PTS 42.
	binary.Write(&stream, binary.BigEndian, flagKeyFrame|42)
	binary.Write(&stream, binary.BigEndian, uint32(2))
	stream.Write([]byte{4, 5})

	r := &stream
	if got, err := readDeviceName(r); err != nil || got != "Pixel 5" {
		t.Fatalf("device name = %q, %v", got, err)
	}
	if err := readCodec(r); err != nil {
		t.Fatal(err)
	}

	var buf []byte
	p, buf, err := readPacket(r, buf)
	if err != nil || !p.Session || p.Width != 1080 || p.Height != 2340 {
		t.Fatalf("session packet = %+v, %v", p, err)
	}
	p, buf, err = readPacket(r, buf)
	if err != nil || !p.Config || p.KeyFrame || !bytes.Equal(p.Data, []byte{1, 2, 3}) {
		t.Fatalf("config packet = %+v, %v", p, err)
	}
	p, _, err = readPacket(r, buf)
	if err != nil || p.Config || !p.KeyFrame || p.PTS != 42 || !bytes.Equal(p.Data, []byte{4, 5}) {
		t.Fatalf("frame packet = %+v, %v", p, err)
	}
	if _, _, err := readPacket(r, buf); err != io.EOF {
		t.Fatalf("end of stream: err = %v, want EOF", err)
	}
}

func TestReadCodecErrors(t *testing.T) {
	for _, id := range []uint32{codecDisabled, codecError, 0x00617631 /* av1 */} {
		var b bytes.Buffer
		binary.Write(&b, binary.BigEndian, id)
		if err := readCodec(&b); err == nil {
			t.Errorf("codec 0x%08x accepted", id)
		}
	}
}

func TestTailBuffer(t *testing.T) {
	tb := &tailBuffer{max: 10}
	tb.Write([]byte("0123456789"))
	tb.Write([]byte("abc"))
	if got := tb.String(); got != "3456789abc" {
		t.Errorf("tail = %q", got)
	}
}
