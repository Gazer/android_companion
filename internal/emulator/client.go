// Package emulator wraps the gRPC EmulatorController service exposed by the
// Android emulator when launched with -grpc <port>.
package emulator

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/rmarkiewicz/android-companion/gen/emulatorpb"
)

// Client talks to the emulator gRPC bridge.
type Client struct {
	conn   *grpc.ClientConn
	ctrl   pb.EmulatorControllerClient
	Width  uint32
	Height uint32
}

// Connect dials the emulator gRPC endpoint.
func Connect(ctx context.Context, port int) (*Client, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	// Use a block dial with timeout so we fail fast if the port isn't ready.
	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(dialCtx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		// Screenshots can be large: a 2208x1840 RGB888 frame is ~12MB.
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(64*1024*1024)),
	)
	if err != nil {
		return nil, fmt.Errorf("dial emulator grpc: %w", err)
	}
	return &Client{conn: conn, ctrl: pb.NewEmulatorControllerClient(conn)}, nil
}

// Close tears down the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Frame is a single decoded screenshot.
type Frame struct {
	Width, Height uint32
	Pixels        []byte // RGB888 packed, row-major, bottom-up as per proto doc
	Seq           uint32
}

// StreamFrames subscribes to the screenshot stream and invokes cb for every frame.
// It runs until ctx is cancelled or the stream errors.
// The stream is requested at a bounded width to keep bandwidth sane (native
// device res can be 2208x1840 = ~12MB/frame in RGB888).
func (c *Client) StreamFrames(ctx context.Context, cb func(Frame)) error {
	format := &pb.ImageFormat{
		Format: pb.ImageFormat_RGB888,
		Width:  640, // request scaled frames; aspect ratio preserved by server
	}
	stream, err := c.ctrl.StreamScreenshot(ctx, format)
	if err != nil {
		return fmt.Errorf("stream screenshot: %w", err)
	}

	var first sync.Once
	for {
		img, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("recv frame: %w", err)
		}
		f := Frame{
			Width:  img.GetFormat().GetWidth(),
			Height: img.GetFormat().GetHeight(),
			Pixels: img.GetImage(),
			Seq:    img.GetSeq(),
		}
		first.Do(func() {
			c.Width, c.Height = f.Width, f.Height
		})
		cb(f)
	}
}

// SendMouse forwards a mouse/touch event to the device.
// x,y are in device display coordinates. buttons: 0=none, 1=primary, 2=secondary.
func (c *Client) SendMouse(ctx context.Context, x, y int32, buttons int32) error {
	ev := &pb.MouseEvent{
		X:       x,
		Y:       y,
		Buttons: buttons,
	}
	_, err := c.ctrl.SendMouse(ctx, ev)
	return err
}

// SendKey forwards a key event using the evdev/Linux keycode (ADDB keycode type).
// For printable ASCII prefer SendText.
func (c *Client) SendKey(ctx context.Context, keyCode int32) error {
	ev := &pb.KeyboardEvent{
		CodeType:  pb.KeyboardEvent_Evdev,
		EventType: pb.KeyboardEvent_keypress,
		KeyCode:   keyCode,
	}
	_, err := c.ctrl.SendKey(ctx, ev)
	return err
}

// SendText sends printable characters (simpler and more reliable than raw keycodes).
func (c *Client) SendText(ctx context.Context, text string) error {
	ev := &pb.KeyboardEvent{Text: text}
	_, err := c.ctrl.SendKey(ctx, ev)
	return err
}

// SendKeyName presses a key given its W3C name ("Enter", "Backspace",
// "ArrowUp", "GoBack"...).
func (c *Client) SendKeyName(ctx context.Context, key string) error {
	ev := &pb.KeyboardEvent{EventType: pb.KeyboardEvent_keypress, Key: key}
	_, err := c.ctrl.SendKey(ctx, ev)
	return err
}
