package logcat

import (
	"strconv"
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

func TestCollectIncremental(t *testing.T) {
	s := NewStore("", "")
	for i := 0; i < 5; i++ {
		s.Add(Entry{Raw: strconv.Itoa(i), PID: strconv.Itoa(100 + i%2)})
	}

	all, next := s.Collect(0, nil)
	if len(all) != 5 || next != 5 {
		t.Fatalf("Collect(0) = %d entries, next %d; want 5, 5", len(all), next)
	}
	if got, n := s.Collect(next, nil); len(got) != 0 || n != 5 {
		t.Fatalf("Collect(next) = %d entries, next %d; want 0, 5", len(got), n)
	}

	s.Add(Entry{Raw: "5", PID: "100"})
	s.Add(Entry{Raw: "6", PID: "101"})
	odd, next := s.Collect(next, func(e *Entry) bool { return e.PIDNum() == 101 })
	if len(odd) != 1 || odd[0].Raw != "6" || next != 7 {
		t.Fatalf("filtered Collect = %+v, next %d; want [6], 7", odd, next)
	}

	// Clear keeps sequence numbers growing.
	s.Clear()
	s.Add(Entry{Raw: "7"})
	if got, n := s.Collect(next, nil); len(got) != 1 || got[0].Raw != "7" || n != 8 {
		t.Fatalf("Collect after Clear = %+v, next %d; want [7], 8", got, n)
	}
}

func TestCollectLast(t *testing.T) {
	s := NewStore("", "")
	for i := 0; i < 10; i++ {
		s.Add(Entry{Raw: strconv.Itoa(i)})
	}
	even := func(e *Entry) bool { n, _ := strconv.Atoi(e.Raw); return n%2 == 0 }
	got, next := s.CollectLast(3, even)
	if len(got) != 3 || got[0].Raw != "4" || got[1].Raw != "6" || got[2].Raw != "8" || next != 10 {
		t.Fatalf("CollectLast(3, even) = %+v, next %d; want [4 6 8], 10", got, next)
	}
	if got, _ := s.CollectLast(100, nil); len(got) != 10 || got[0].Raw != "0" {
		t.Fatalf("CollectLast(100, nil) = %d entries starting at %q", len(got), got[0].Raw)
	}
}

func TestCollectAfterEviction(t *testing.T) {
	s := NewStore("", "")
	for i := 0; i < maxStoredEntries+1; i++ {
		s.Add(Entry{Raw: strconv.Itoa(i)})
	}
	// Reading from 0 must start at the oldest retained entry.
	got, next := s.Collect(0, nil)
	if len(got) != s.Len() {
		t.Fatalf("Collect(0) returned %d entries, want %d", len(got), s.Len())
	}
	if want := strconv.Itoa(maxStoredEntries / 10); got[0].Raw != want {
		t.Errorf("first entry = %q, want %q", got[0].Raw, want)
	}
	if next != maxStoredEntries+1 {
		t.Errorf("next = %d, want %d", next, maxStoredEntries+1)
	}
}

func TestSetProcessMapVersion(t *testing.T) {
	s := NewStore("", "")
	apps, seen := parseProcessList("PID NAME\n1 init\n42 system_server\n100 com.example.app\n101 com.example.app:remote\n")
	if _, ok := seen[42]; !ok {
		t.Fatalf("system_server PID not in seen set: %v", seen)
	}
	if _, ok := apps[42]; ok {
		t.Fatalf("system_server must not be an app: %v", apps)
	}

	v0 := s.MapVersion()
	s.setProcessMap(apps, seen)
	v1 := s.MapVersion()
	if v1 == v0 {
		t.Fatal("MapVersion did not change after a new map")
	}
	s.setProcessMap(parseProcessList("100 com.example.app\n101 com.example.app:remote\n"))
	if s.MapVersion() != v1 {
		t.Fatal("MapVersion changed for an identical map")
	}

	pids := s.PIDsForPackage("com.example.app")
	if _, ok := pids[100]; !ok || len(pids) != 1 {
		t.Fatalf("PIDsForPackage = %v, want {100}", pids)
	}
}

func TestParseProcessListToolbox(t *testing.T) {
	// Android < 8: plain `ps`, PID in the second column.
	out := "USER     PID   PPID  VSIZE  RSS     WCHAN    PC         NAME\r\n" +
		"root      1     0     8904   788   ffffffff 00000000 S /init\r\n" +
		"system    512   300   1702500 90000 ffffffff 00000000 S system_server\r\n" +
		"u0_a12    1234  300   1500000 60000 ffffffff 00000000 S com.example.app\r\n"
	apps, seen := parseProcessList(out)
	if len(seen) != 3 {
		t.Fatalf("seen = %v, want 3 PIDs", seen)
	}
	if apps[1234] != "com.example.app" || len(apps) != 1 {
		t.Fatalf("apps = %v, want {1234: com.example.app}", apps)
	}
}

func TestSetDeviceDropsPreviousMap(t *testing.T) {
	s := NewStore("", "")
	s.setProcessMap(parseProcessList("100 com.example.app\n"))
	s.SetDevice("", "other-serial") // no adb: the refresh is a no-op
	if pids := s.PIDsForPackage("com.example.app"); len(pids) != 0 {
		t.Fatalf("PIDs from the previous device survived: %v", pids)
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
