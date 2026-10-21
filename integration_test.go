//go:build integration

// Run with: go test -tags=integration -timeout 5m ./...
// Launches a real emulator, connects gRPC, verifies frames arrive, then kills it.
package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rmarkiewicz/android-companion/internal/avd"
	"github.com/rmarkiewicz/android-companion/internal/emulator"
)

func TestEmulatorGRPCStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	mgr, err := avd.NewManager()
	if err != nil {
		t.Skipf("no SDK: %v", err)
	}
	avds, err := mgr.ListAVDs(ctx)
	if err != nil || len(avds) == 0 {
		t.Skipf("no AVDs available")
	}
	t.Logf("using AVD %s", avds[0])

	inst, err := mgr.Launch(ctx, avds[0], 18554, false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer inst.Kill()

	client, err := emulator.Connect(ctx, 18554)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	// Log the actual frame dimensions we get back
	t.Log("waiting for frames...")

	var frames int64
	streamCtx, stopStream := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- client.StreamFrames(streamCtx, func(f emulator.Frame) {
			n := atomic.AddInt64(&frames, 1)
			if n == 1 {
				t.Logf("first frame: %dx%d (%d bytes)", f.Width, f.Height, len(f.Pixels))
				if f.Width == 0 || f.Height == 0 || len(f.Pixels) == 0 {
					t.Errorf("empty frame")
				}
			}
			if n >= 5 {
				stopStream()
			}
		})
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Logf("stream ended with error: %v", err)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("timeout waiting for frames")
	}
	if atomic.LoadInt64(&frames) < 5 {
		t.Fatalf("only got %d frames", frames)
	}
	t.Logf("received %d frames OK", frames)
}
