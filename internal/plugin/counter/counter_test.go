package counter

import (
	"sync"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/rmarkiewicz/android-companion/internal/logcat"
)

func newTestCounter(t *testing.T, pattern string) *Plugin {
	t.Helper()
	test.NewTempApp(t)
	p, err := New("crashes", pattern, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.schedule = func(f func()) { f() } // refresh synchronously
	return p
}

func line(s string) logcat.Entry { return logcat.ParseLine(s) }

func TestCounterCountsMatchingLines(t *testing.T) {
	p := newTestCounter(t, "FATAL EXCEPTION")

	p.ProcessLine(line("10-21 12:00:00.000  1234  1234 E AndroidRuntime: FATAL EXCEPTION: main"))
	p.ProcessLine(line("10-21 12:00:00.001  1234  1234 E AndroidRuntime: Process: com.example, PID: 1234"))
	p.ProcessLine(line("10-21 12:00:01.000  1234  1234 E AndroidRuntime: FATAL EXCEPTION: worker"))
	p.ProcessLine(line("10-21 12:00:02.000  1234  1234 E AndroidRuntime: fatal exception (lowercase)"))

	if got := p.Count(); got != 2 {
		t.Fatalf("Count = %d, want 2", got)
	}
	if p.countText.Text != "2" {
		t.Fatalf("label = %q, want 2", p.countText.Text)
	}
}

func TestCounterMatchesTagAndPlaceholders(t *testing.T) {
	p := newTestCounter(t, "MyTag: frame {%d}ms")
	p.ProcessLine(line("10-21 12:00:00.000  1234  1234 I MyTag: frame 16ms"))
	p.ProcessLine(line("10-21 12:00:00.000  1234  1234 I MyTag: frame slow"))
	p.ProcessLine(line("raw line without header MyTag: frame 33ms"))
	if got := p.Count(); got != 2 {
		t.Fatalf("Count = %d, want 2", got)
	}
}

func TestCounterReset(t *testing.T) {
	p := newTestCounter(t, "tick")
	for i := 0; i < 5; i++ {
		p.ProcessLine(line("tick"))
	}
	test.Tap(p.resetBtn)
	if p.Count() != 0 || p.countText.Text != "0" {
		t.Fatalf("after reset: count %d, label %q", p.Count(), p.countText.Text)
	}
	p.ProcessLine(line("tick"))
	if p.Count() != 1 || p.countText.Text != "1" {
		t.Fatalf("after reset + 1: count %d, label %q", p.Count(), p.countText.Text)
	}
}

func TestCounterConcurrent(t *testing.T) {
	test.NewTempApp(t)
	p, err := New("n", "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	pending := 0
	p.schedule = func(func()) { pending++ } // count refresh requests, don't run them

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				p.ProcessLine(logcat.Entry{Raw: "x"})
			}
		}()
	}
	wg.Wait()
	if got := p.Count(); got != 8000 {
		t.Fatalf("Count = %d, want 8000", got)
	}
	// Refreshes are coalesced: one pending at a time.
	if pending != 1 {
		t.Fatalf("scheduled %d refreshes, want 1", pending)
	}
}

func TestCounterValidation(t *testing.T) {
	test.NewTempApp(t)
	if _, err := New("x", "", nil); err == nil {
		t.Error("empty pattern accepted")
	}
	if _, err := New("x", "bad {%q}", nil); err == nil {
		t.Error("unknown placeholder accepted")
	}
}

func TestFormatCount(t *testing.T) {
	tests := map[int64]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567", 100000: "100,000"}
	for n, want := range tests {
		if got := formatCount(n); got != want {
			t.Errorf("formatCount(%d) = %q, want %q", n, got, want)
		}
	}
}
