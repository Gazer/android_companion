package logcat

import (
	"testing"
)

func TestParsePID(t *testing.T) {
	tests := []struct {
		in   string
		want int32
	}{
		{"1234", 1234},
		{" 567 ", 567},
		{"abc", -1},
		{"", -1},
	}
	for _, tt := range tests {
		if got := parsePID(tt.in); got != tt.want {
			t.Errorf("parsePID(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestStoreAddAndFilter(t *testing.T) {
	s := NewStore("", "")
	// Simulate a known PID->package mapping
	s.mu.Lock()
	s.pidToPackage[1234] = "com.example.app"
	s.packageSet["com.example.app"] = struct{}{}
	s.mu.Unlock()

	e := Entry{Raw: "01-01 12:00:00.000  1234  5678 I MyTag: hello", PID: "1234"}
	s.Add(e)

	if got := s.Len(); got != 1 {
		t.Fatalf("Len() = %d, want 1", got)
	}

	filtered := s.FilterByPackage("com.example.app")
	if len(filtered) != 1 {
		t.Fatalf("FilterByPackage returned %d entries, want 1", len(filtered))
	}

	all := s.FilterByPackage("")
	if len(all) != 1 {
		t.Fatalf("FilterByPackage(\"\") returned %d entries, want 1", len(all))
	}

	pkgs := s.Packages()
	if len(pkgs) != 1 || pkgs[0] != "com.example.app" {
		t.Fatalf("Packages() = %v, want [com.example.app]", pkgs)
	}
}

func TestContinuationLineInheritance(t *testing.T) {
	s := NewStore("", "")
	s.mu.Lock()
	s.pidToPackage[1234] = "com.example.app"
	s.packageSet["com.example.app"] = struct{}{}
	s.mu.Unlock()

	// Parent line with full header
	s.Add(Entry{
		Raw:   "01-01 12:00:00.000  1234  5678 D MyTag: statistics:",
		Time:  "01-01 12:00:00.000",
		PID:   "1234",
		TID:   "5678",
		Level: "D",
		Tag:   "MyTag",
	})
	// Continuation line: no parsed fields (like logcat emits for multi-line messages)
	s.Add(Entry{Raw: "run from = 516 ms"})
	s.Add(Entry{Raw: "total sent = 47 bytes"})

	// Continuations must inherit PID/Level from parent
	entries := s.Snapshot()
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	for i := 1; i < 3; i++ {
		if entries[i].PID != "1234" {
			t.Errorf("entries[%d].PID = %q, want 1234 (inherited)", i, entries[i].PID)
		}
		if entries[i].Level != "D" {
			t.Errorf("entries[%d].Level = %q, want D (inherited)", i, entries[i].Level)
		}
		if entries[i].Time != "" {
			t.Errorf("entries[%d].Time = %q, want empty (continuation)", i, entries[i].Time)
		}
	}

	// Continuations must pass the package filter
	filtered := s.FilterByPackage("com.example.app")
	if len(filtered) != 3 {
		t.Fatalf("FilterByPackage returned %d entries, want 3 (continuations included)", len(filtered))
	}
}

func TestParseLinePreservesMessageIndentation(t *testing.T) {
	// Multi-line log messages keep their original leading spaces; only the
	// single separating space after "TAG:" is stripped.
	line := `10-21 12:03:49.589  3011  3777 D CSLIB|SrEventsDispatcher:    run from = 1158 ms`
	e := ParseLine(line)
	if e.Time == "" {
		t.Fatalf("line did not parse as threadtime: %+v", e)
	}
	if e.Tag != "CSLIB|SrEventsDispatcher" {
		t.Errorf("Tag = %q, want CSLIB|SrEventsDispatcher", e.Tag)
	}
	want := "   run from = 1158 ms" // 4 spaces minus 1 separator = 3
	if e.Message != want {
		t.Errorf("Message = %q, want %q (indentation preserved)", e.Message, want)
	}

	// No space after colon at all
	line2 := `10-21 12:03:49.589  3011  3777 D Tag:message`
	e2 := ParseLine(line2)
	if e2.Message != "message" {
		t.Errorf("Message = %q, want %q", e2.Message, "message")
	}
}

func TestIsInterestingProcess(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"com.example.app", true},
		{"com.example.app:remote", true},
		{"system_server", false},
		{"[kworker]", false},
		{"init", false},
		{"adbd", false},
		{"sh", false},
		{"logd", false},
	}
	for _, tt := range tests {
		if got := isInterestingProcess(tt.name); got != tt.want {
			t.Errorf("isInterestingProcess(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
