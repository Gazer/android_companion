package ui

import (
	"fmt"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/rmarkiewicz/android-companion/internal/logcat"
)

type testLogs struct {
	store *logcat.Store
	n     int
}

// add appends n log lines with distinct timestamps (so each one shows its
// header) and the given message prefix.
func (l *testLogs) add(n int, msg string) {
	for i := 0; i < n; i++ {
		l.n++
		l.store.Add(logcat.ParseLine(fmt.Sprintf(
			"10-21 12:%02d:%02d.%03d  1234  1234 I MyTag: %s %d",
			l.n/60000%60, l.n/1000%60, l.n%1000, msg, l.n)))
	}
}

func newTestPanel(t testing.TB) (*LogcatPanel, *testLogs, fyne.Window) {
	t.Helper()
	test.NewTempApp(t)
	store := logcat.NewStore("", "")
	p := NewLogcatPanel(store)
	w := test.NewWindow(p.Content())
	w.Resize(fyne.NewSize(900, 400))
	t.Cleanup(w.Close)
	return p, &testLogs{store: store}, w
}

// tablePos is a point over the log table in window coordinates.
var tablePos = fyne.NewPos(300, 200)

func assertFollowing(t *testing.T, p *LogcatPanel) {
	t.Helper()
	p.measureVisible()
	if p.vis.max != len(p.rows)-1 {
		t.Fatalf("last visible row = %d, want %d (newest)", p.vis.max, len(p.rows)-1)
	}
	if !p.following || p.bottomBtn.Visible() {
		t.Fatalf("following = %v, jump button visible = %v; want true, false", p.following, p.bottomBtn.Visible())
	}
}

func TestLogcatPanelFollowsNewLogs(t *testing.T) {
	p, logs, _ := newTestPanel(t)

	logs.add(5, "short")
	p.RefreshDisplay()
	assertFollowing(t, p)

	// From "fits in the view" to "overflows".
	logs.add(200, "more")
	p.RefreshDisplay()
	assertFollowing(t, p)

	logs.add(50, "more")
	p.RefreshDisplay()
	assertFollowing(t, p)

	if got := p.statusLabel.Text; got != "255 logs" {
		t.Errorf("status = %q, want %q", got, "255 logs")
	}
}

func TestLogcatPanelPausesWhenScrolledUp(t *testing.T) {
	p, logs, w := newTestPanel(t)
	logs.add(300, "line")
	p.RefreshDisplay()
	assertFollowing(t, p)

	// The user scrolls up: following stops right away.
	test.Scroll(w.Canvas(), tablePos, 0, 2000)
	if p.following || !p.bottomBtn.Visible() {
		t.Fatalf("after scrolling up: following = %v, jump button visible = %v", p.following, p.bottomBtn.Visible())
	}
	p.measureVisible()
	top := p.vis.min

	// New logs don't move the view.
	logs.add(100, "line")
	p.RefreshDisplay()
	p.measureVisible()
	if p.vis.min != top {
		t.Fatalf("first visible row moved from %d to %d while paused", top, p.vis.min)
	}
	if !p.bottomBtn.Visible() {
		t.Fatal("jump button hidden while paused")
	}

	test.Tap(p.bottomBtn)
	assertFollowing(t, p)

	// Scrolling back to the bottom by hand also resumes following.
	test.Scroll(w.Canvas(), tablePos, 0, 500)
	if p.following {
		t.Fatal("still following after scrolling up")
	}
	test.Scroll(w.Canvas(), tablePos, 0, -100000)
	if !p.following || p.bottomBtn.Visible() {
		t.Fatal("not following after scrolling back to the bottom")
	}
}

func TestLogcatPanelKeepsHorizontalOffset(t *testing.T) {
	p, logs, w := newTestPanel(t)
	logs.add(100, strings.Repeat("wide ", 100))
	p.RefreshDisplay()

	test.Scroll(w.Canvas(), tablePos, -500, 0) // scroll right
	before, ok := tableOffset(p.table)
	if !ok {
		t.Fatal("tableOffset can't read widget.Table's offset (Fyne changed?)")
	}
	if before.X <= 0 {
		t.Fatalf("horizontal offset = %v after scrolling right", before.X)
	}

	logs.add(20, "new")
	p.RefreshDisplay()
	assertFollowing(t, p)
	if after, _ := tableOffset(p.table); after.X != before.X {
		t.Fatalf("horizontal offset changed from %v to %v when following", before.X, after.X)
	}
}

func TestLogcatPanelTextFilter(t *testing.T) {
	p, logs, _ := newTestPanel(t)
	logs.add(10, "alpha")
	logs.add(10, "beta")
	p.RefreshDisplay()

	p.filterEntry.SetText("BETA")
	if len(p.rows) != 10 {
		t.Fatalf("rows after filter = %d, want 10", len(p.rows))
	}

	// New logs are filtered incrementally.
	logs.add(5, "beta")
	logs.add(5, "alpha")
	p.RefreshDisplay()
	if len(p.rows) != 15 {
		t.Fatalf("rows after new logs = %d, want 15", len(p.rows))
	}
	if got := p.statusLabel.Text; got != "15 / 30 logs" {
		t.Errorf("status = %q, want %q", got, "15 / 30 logs")
	}

	p.filterEntry.SetText("")
	if len(p.rows) != 30 {
		t.Fatalf("rows after clearing the filter = %d, want 30", len(p.rows))
	}

	// "~text" excludes, also for new logs.
	p.filterEntry.SetText("~Alpha")
	if len(p.rows) != 15 {
		t.Fatalf("rows with ~Alpha = %d, want 15", len(p.rows))
	}
	for _, r := range p.rows {
		if strings.Contains(r.msg, "alpha") {
			t.Fatalf("~Alpha kept %q", r.msg)
		}
	}
	logs.add(3, "alpha")
	logs.add(2, "gamma")
	p.RefreshDisplay()
	if len(p.rows) != 17 {
		t.Fatalf("rows with ~Alpha after new logs = %d, want 17", len(p.rows))
	}

	// A lone "~" filters nothing.
	p.filterEntry.SetText("~")
	if len(p.rows) != 35 {
		t.Fatalf("rows with ~ = %d, want 35", len(p.rows))
	}

	// Several terms: all must match (anywhere in the line, tag included).
	p.filterEntry.SetText("~alpha ~gamma")
	if len(p.rows) != 15 {
		t.Fatalf("rows with ~alpha ~gamma = %d, want 15 (only beta)", len(p.rows))
	}
	p.filterEntry.SetText("MyTag beta")
	if len(p.rows) != 15 {
		t.Fatalf("rows with 'MyTag beta' = %d, want 15", len(p.rows))
	}
	p.filterEntry.SetText("beta gamma")
	if len(p.rows) != 0 {
		t.Fatalf("rows with 'beta gamma' = %d, want 0", len(p.rows))
	}
}

func TestLogcatPanelRestoresPackageAndFilter(t *testing.T) {
	a := test.NewTempApp(t)
	store := logcat.NewStore("", "")

	// Changes are saved...
	p := NewLogcatPanel(store)
	p.filterEntry.SetText("error ~gc")
	p.packageFilter.onSelect("com.example.app")
	prefs := a.Preferences()
	if got := prefs.String(prefFilter); got != "error ~gc" {
		t.Fatalf("saved filter = %q", got)
	}
	if got := prefs.String(prefPackage); got != "com.example.app" {
		t.Fatalf("saved package = %q", got)
	}

	// ...and restored by the next panel (i.e. on the next start).
	p2 := NewLogcatPanel(store)
	if p2.filterEntry.Text != "error ~gc" || len(p2.filter.include) != 1 || len(p2.filter.exclude) != 1 {
		t.Fatalf("filter not restored: entry %q, parsed %+v", p2.filterEntry.Text, p2.filter)
	}
	if p2.pkg != "com.example.app" || p2.packageFilter.Selected() != "com.example.app" ||
		p2.packageFilter.button.Text != "com.example.app" {
		t.Fatalf("package not restored: pkg %q, selected %q, button %q",
			p2.pkg, p2.packageFilter.Selected(), p2.packageFilter.button.Text)
	}

	// "Clear (show all)" is saved too.
	p2.packageFilter.onSelect("")
	if got := prefs.String(prefPackage); got != "" {
		t.Fatalf("saved package after clearing = %q", got)
	}
}

func TestLogcatPanelTrimsOldRows(t *testing.T) {
	p, logs, w := newTestPanel(t)

	// While following, rows are capped at maxDisplayedRows.
	logs.add(maxDisplayedRows+1, "x")
	p.RefreshDisplay()
	if len(p.rows) > maxDisplayedRows {
		t.Fatalf("rows = %d, want <= %d", len(p.rows), maxDisplayedRows)
	}
	assertFollowing(t, p)

	// While paused, trimming keeps the same logs on screen.
	test.Scroll(w.Canvas(), tablePos, 0, 5000)
	p.measureVisible()
	topMsg := p.rows[p.vis.min+1].msg
	logs.add(maxDisplayedRowsPaused-len(p.rows)+1, "y")
	p.RefreshDisplay()
	if len(p.rows) > maxDisplayedRowsPaused {
		t.Fatalf("rows = %d, want <= %d", len(p.rows), maxDisplayedRowsPaused)
	}
	if p.following {
		t.Fatal("trimming resumed following")
	}
	p.measureVisible()
	if got := p.rows[p.vis.min+1].msg; got != topMsg {
		t.Fatalf("visible log changed after trimming: %q, want %q", got, topMsg)
	}
}

func TestRowBuilderHidesRepeatedHeaders(t *testing.T) {
	test.NewTempApp(t)
	b := newRowBuilder(newLogMetrics(14))
	lines := []string{
		"10-21 12:00:00.000  1234  1234 D MyTag: statistics:",
		"10-21 12:00:00.000  1234  1234 D MyTag:   run from = 516 ms",
		"10-21 12:00:00.001  1234  1234 D MyTag: next",
		"--------- beginning of main",
		"10-21 12:00:00.001  1234  1234 D MyTag: after raw line",
	}
	want := []bool{true, false, true, false, true}
	for i, line := range lines {
		e := logcat.ParseLine(line)
		r := b.build(&e)
		if got := r.parsed && r.showHeader; got != want[i] {
			t.Errorf("line %d header shown = %v, want %v", i, got, want[i])
		}
	}
	if b.maxMsgW <= 0 {
		t.Error("maxMsgW not tracked")
	}
}

func TestContainsFold(t *testing.T) {
	tests := []struct {
		s, sub string
		want   bool
	}{
		{"Hello World", "world", true},
		{"Hello World", "", true},
		{"Hello", "hello!", false},
		{"ERROR: boom", "error", true},
		{"ÑANDÚ grande", "ñandú", true},
		{"abc", "x", false},
	}
	for _, tt := range tests {
		if got := containsFold(tt.s, tt.sub); got != tt.want {
			t.Errorf("containsFold(%q, %q) = %v, want %v", tt.s, tt.sub, got, tt.want)
		}
	}
}

func TestTruncateMiddle(t *testing.T) {
	if got := truncateMiddle("short", 22); got != "short" {
		t.Errorf("got %q", got)
	}
	got := truncateMiddle("CSLIB|SessionReplayReporter", 22)
	if len([]rune(got)) != 22 || !strings.Contains(got, "...") {
		t.Errorf("truncateMiddle = %q, want 22 runes with ...", got)
	}
	// Must not split multi-byte characters.
	got = truncateMiddle(strings.Repeat("ñ", 30), 22)
	if len([]rune(got)) != 22 || !strings.HasPrefix(got, "ñ") {
		t.Errorf("truncateMiddle = %q, want 22 runes", got)
	}
}
