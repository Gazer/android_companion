package logcat

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeAdb writes a script that ignores its arguments, prints script and exits.
func fakeAdb(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "adb")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStreamerClosesChannelWhenAdbExits(t *testing.T) {
	adb := fakeAdb(t, "echo '10-21 12:00:00.000  1234  1234 I MyTag: hello'\n")
	ch, err := NewStreamer(adb, "serial").Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	var got []Entry
	timeout := time.After(5 * time.Second)
	for {
		select {
		case e, ok := <-ch:
			if !ok {
				if len(got) != 1 || got[0].Message != "hello" {
					t.Fatalf("entries = %+v, want one 'hello'", got)
				}
				return
			}
			got = append(got, e)
		case <-timeout:
			t.Fatal("channel not closed after adb exited (device disconnect would go unnoticed)")
		}
	}
}

func TestStreamerStop(t *testing.T) {
	adb := fakeAdb(t, "exec sleep 30\n")
	s := NewStreamer(adb, "serial")
	ch, err := s.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s.Stop()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("unexpected entry")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("channel not closed after Stop")
	}
}
