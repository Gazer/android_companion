package chart

import (
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/rmarkiewicz/android-companion/internal/logcat"
)

func TestCompilePattern(t *testing.T) {
	re, err := CompilePattern("Session Replay telemetry: sr_perf_01={%d}ms")
	if err != nil {
		t.Fatalf("CompilePattern failed: %v", err)
	}

	line := "Session Replay telemetry: sr_perf_01=3ms, sr_perf_02=1ms"
	m := re.FindStringSubmatch(line)
	if m == nil {
		t.Fatal("pattern did not match")
	}
	if m[1] != "3" {
		t.Errorf("captured %q, want %q", m[1], "3")
	}
}

func TestCompilePatternFloat(t *testing.T) {
	re, err := CompilePattern("fps: {%f}")
	if err != nil {
		t.Fatalf("CompilePattern failed: %v", err)
	}
	m := re.FindStringSubmatch("fps: 59.7")
	if m == nil || m[1] != "59.7" {
		t.Errorf("float capture failed: %v", m)
	}
}

func TestCompilePatternErrors(t *testing.T) {
	if _, err := CompilePattern("unclosed {%d"); err == nil {
		t.Error("expected error for unclosed placeholder")
	}
	if _, err := CompilePattern("bad {%x}"); err == nil {
		t.Error("expected error for unknown placeholder")
	}
}

func TestPluginProcessLine(t *testing.T) {
	test.NewApp() // widgets need a Fyne app context
	p, err := New("test", "sr_perf_01={%d}ms", func() {})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// Non-matching line
	p.ProcessLine(logcat.Entry{Message: "something else"})
	if len(p.points) != 0 {
		t.Errorf("expected 0 points, got %d", len(p.points))
	}

	// Matching line with timestamp
	p.ProcessLine(logcat.Entry{
		Time:    "10-21 12:24:06.457",
		Message: "Session Replay telemetry: sr_perf_01=3ms, sr_perf_02=1ms",
	})
	if len(p.points) != 1 {
		t.Fatalf("expected 1 point, got %d", len(p.points))
	}
	if p.points[0].v != 3 {
		t.Errorf("value = %v, want 3", p.points[0].v)
	}
	if p.points[0].t == 0 {
		t.Error("timestamp not parsed")
	}
}
