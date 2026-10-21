package ui

import (
	"strconv"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/rmarkiewicz/android-companion/internal/logcat"
)

const (
	// maxDisplayedEntries caps how many items the list widget handles.
	maxDisplayedEntries = 1000
)

// wideList wraps a CanvasObject (the list+interceptor) and overrides
// MinSize to report the full width needed by the longest visible row, so
// an outer horizontal scroller can actually scroll. The List itself
// always clamps rows to its own width, so we resize it ourselves.
type wideList struct {
	widget.BaseWidget
	content  fyne.CanvasObject
	minWidth float32
}

func newWideList(content fyne.CanvasObject) *wideList {
	w := &wideList{content: content, minWidth: 0}
	w.ExtendBaseWidget(w)
	return w
}

// setMinWidth updates the reported content width and resizes the list if
// it exceeds the visible area. Called when displayed entries change.
func (w *wideList) setMinWidth(width float32) {
	w.minWidth = width
	cur := w.Size()
	if width > cur.Width {
		w.Resize(fyne.NewSize(width, cur.Height))
	}
	w.Refresh()
}

func (w *wideList) CreateRenderer() fyne.WidgetRenderer {
	return &wideListRenderer{w: w}
}

type wideListRenderer struct {
	w *wideList
}

func (r *wideListRenderer) Layout(size fyne.Size) {
	w := size.Width
	if r.w.minWidth > w {
		w = r.w.minWidth
	}
	r.w.content.Resize(fyne.NewSize(w, size.Height))
}

func (r *wideListRenderer) MinSize() fyne.Size {
	s := r.w.content.MinSize()
	if r.w.minWidth > s.Width {
		s.Width = r.w.minWidth
	}
	return s
}

func (r *wideListRenderer) Refresh() {
	r.w.content.Refresh()
}

func (r *wideListRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.w.content}
}

func (r *wideListRenderer) Destroy() {}

// scrollInterceptor wraps the List to detect user scrolls by observing
// offset changes between refresh ticks: a decrease means the user scrolled
// up (we never scroll up programmatically), which pauses auto-scroll.
type scrollInterceptor struct {
	widget.BaseWidget
	list       *widget.List
	lastOffset float32
	onUserUp   func()
}

func newScrollInterceptor(list *widget.List, onUserUp func()) *scrollInterceptor {
	s := &scrollInterceptor{list: list, onUserUp: onUserUp}
	s.ExtendBaseWidget(s)
	return s
}

// check compares the current offset to the last known one. We call it
// right after applying our own ScrollToBottom, so lastOffset always
// reflects the settled position; any later decrease can only come from
// the user.
func (s *scrollInterceptor) check() {
	off := s.list.GetScrollOffset()
	if off < s.lastOffset-1 && s.onUserUp != nil {
		s.onUserUp()
	}
	s.lastOffset = off
}

// sync updates lastOffset to the current position without triggering
// detection. Call right after a programmatic ScrollToBottom.
func (s *scrollInterceptor) sync() {
	s.lastOffset = s.list.GetScrollOffset()
}

func (s *scrollInterceptor) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(s.list)
}

// LogcatPanel is the right-side panel with package filter, text filter,
// and a scrollable virtualized log list with Android Studio-style colors.
type LogcatPanel struct {
	widget.BaseWidget

	store *logcat.Store

	mu                sync.RWMutex
	displayed         []logcat.Entry
	packageFilterName string
	textFilter        string

	// UI widgets
	packageFilter *PackageFilter
	filterEntry   *widget.Entry
	list          *widget.List
	scroller      *scrollInterceptor
	wide          *wideList
	bottomBtn     *widget.Button
	listArea      *fyne.Container
	statusLabel   *widget.Label

	autoScroll bool

	// Track last state to avoid unnecessary rebuilds
	lastCount   int
	lastFilter  string
	lastPackage string
}

// NewLogcatPanel creates the panel.
func NewLogcatPanel(store *logcat.Store) *LogcatPanel {
	p := &LogcatPanel{store: store, autoScroll: true}

	p.packageFilter = NewPackageFilter(func(pkg string) {
		p.mu.Lock()
		p.packageFilterName = pkg
		p.mu.Unlock()
		p.refreshDisplay()
	})

	p.filterEntry = widget.NewEntry()
	p.filterEntry.SetPlaceHolder("Filter text...")
	p.filterEntry.OnChanged = func(s string) {
		p.mu.Lock()
		p.textFilter = s
		p.mu.Unlock()
		p.refreshDisplay()
	}

	p.statusLabel = widget.NewLabel("Select a package to view logs")

	p.list = widget.NewList(
		func() int {
			p.mu.RLock()
			defer p.mu.RUnlock()
			return len(p.displayed)
		},
		func() fyne.CanvasObject {
			return NewLogLine()
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			p.mu.RLock()
			defer p.mu.RUnlock()
			if id < 0 || id >= len(p.displayed) {
				return
			}
			line := obj.(*LogLine)
			continuation := isContinuation(p.displayed, id)
			line.SetEntry(p.displayed[id], continuation)
		},
	)
	p.list.OnSelected = func(id widget.ListItemID) {
		// noop
	}

	// Scrolling up pauses auto-scroll; the "jump to bottom" button
	// re-enables it. This never fights the user's scrolling.
	p.scroller = newScrollInterceptor(p.list, func() {
		p.mu.Lock()
		was := p.autoScroll
		p.autoScroll = false
		p.mu.Unlock()
		if was {
			p.bottomBtn.Show()
		}
	})

	p.bottomBtn = widget.NewButtonWithIcon("Jump to bottom", theme.MoveDownIcon(), func() {
		p.mu.Lock()
		p.autoScroll = true
		p.mu.Unlock()
		p.bottomBtn.Hide()
		p.list.ScrollToBottom()
		p.scroller.sync()
	})
	p.bottomBtn.Importance = widget.HighImportance
	p.bottomBtn.Hide()

	// Horizontal scroll lets long messages be read: the wideList reports
	// the full content width as its MinSize, so the scroller can scroll.
	p.wide = newWideList(p.scroller)
	hScroll := container.NewHScroll(p.wide)

	p.listArea = container.NewStack(hScroll,
		container.NewVBox(
			layout.NewSpacer(),
			container.NewHBox(layout.NewSpacer(), p.bottomBtn, layout.NewSpacer()),
		),
	)

	p.ExtendBaseWidget(p)
	return p
}

// Content returns the panel's UI tree.
func (p *LogcatPanel) Content() fyne.CanvasObject {
	topBar := container.NewBorder(nil, nil, p.packageFilter.Content(), nil, p.filterEntry)
	header := container.NewVBox(topBar, widget.NewSeparator())
	footer := container.NewHBox(p.statusLabel)

	return container.NewBorder(header, footer, nil, nil, p.listArea)
}

// SetWindow wires the parent window for the package filter dialog.
func (p *LogcatPanel) SetWindow(w fyne.Window) {
	p.packageFilter.SetWindow(w)
}

// RefreshDisplay re-applies filters and updates the list. Call from UI thread.
func (p *LogcatPanel) RefreshDisplay() {
	// Always check for user scrolls, even when no new logs arrived.
	p.scroller.check()

	p.mu.Lock()
	changed := p.textFilter != p.lastFilter ||
		p.packageFilterName != p.lastPackage ||
		p.store.Len() != p.lastCount
	p.mu.Unlock()

	if changed {
		p.refreshDisplay()
		p.refreshPackages()
	}
}

// refreshDisplay recomputes the displayed entries and refreshes the list.
// If no package is selected, nothing is displayed (performance guard).
func (p *LogcatPanel) refreshDisplay() {
	p.mu.Lock()
	if p.packageFilterName == "" {
		p.displayed = nil
		total := p.store.Len()
		p.lastCount = total
		p.lastFilter = p.textFilter
		p.lastPackage = p.packageFilterName
		p.mu.Unlock()

		p.list.Refresh()
		p.statusLabel.SetText("Select a package to view logs")
		return
	}

	packageFiltered := p.store.FilterByPackage(p.packageFilterName)
	if p.textFilter == "" {
		p.displayed = packageFiltered
	} else {
		tf := strings.ToLower(p.textFilter)
		out := packageFiltered[:0]
		for _, e := range packageFiltered {
			if strings.Contains(strings.ToLower(e.Raw), tf) ||
				strings.Contains(strings.ToLower(e.Tag), tf) ||
				strings.Contains(strings.ToLower(e.Message), tf) {
				out = append(out, e)
			}
		}
		p.displayed = out
	}

	if len(p.displayed) > maxDisplayedEntries {
		p.displayed = p.displayed[len(p.displayed)-maxDisplayedEntries:]
	}

	count := len(p.displayed)
	total := p.store.Len()
	p.lastCount = total
	p.lastFilter = p.textFilter
	p.lastPackage = p.packageFilterName
	p.mu.Unlock()

	// Compute the full content width for the horizontal scroller:
	// fixed columns + the longest message rendered in monospace.
	p.wide.setMinWidth(FixedColumnsWidth + p.maxMessageWidth(p.displayed))

	p.list.Refresh()
	p.statusLabel.SetText(formatLogCount(count, total))
	if p.autoScroll && count > 0 {
		p.list.ScrollToBottom()
		// Baseline: after our scroll, only the user can decrease the offset.
		p.scroller.sync()
	}
}

// maxMessageWidth returns the pixel width of the longest message.
func (p *LogcatPanel) maxMessageWidth(entries []logcat.Entry) float32 {
	maxChars := 0
	for _, e := range entries {
		n := len(e.Message)
		if e.Time == "" && len(e.Raw) > n {
			n = len(e.Raw)
		}
		if n > maxChars {
			maxChars = n
		}
	}
	if maxChars == 0 {
		return 0
	}
	// Measure a run of M's: monospace, so it's exact per-char.
	m := strings.Repeat("M", maxChars)
	return fyne.MeasureText(m, theme.TextSize(), fyne.TextStyle{Monospace: true}).Width
}

// refreshPackages updates the package list from the store's index.
func (p *LogcatPanel) refreshPackages() {
	packages := p.store.Packages()
	p.packageFilter.SetPackages(packages)
}

// SetAutoScroll toggles following the tail.
func (p *LogcatPanel) SetAutoScroll(b bool) {
	p.mu.Lock()
	p.autoScroll = b
	p.mu.Unlock()
}

// Clear wipes the display (not the store).
func (p *LogcatPanel) Clear() {
	p.mu.Lock()
	p.displayed = nil
	p.mu.Unlock()
	p.list.Refresh()
	p.statusLabel.SetText("Select a package to view logs")
}

// CreateRenderer is required by fyne.Widget.
func (p *LogcatPanel) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.Content())
}

// isContinuation reports whether entries[id] is a physical continuation of
// entries[id-1]: logcat repeats the full header (timestamp, pid, tid, tag)
// on every physical line of a multi-line message, but Android Studio only
// draws the header on the first one.
func isContinuation(entries []logcat.Entry, id int) bool {
	if id <= 0 || id >= len(entries) {
		return false
	}
	cur, prev := entries[id], entries[id-1]
	if cur.Time == "" {
		return true // unparsed line: always a continuation visually
	}
	return cur.Time == prev.Time &&
		cur.PID == prev.PID &&
		cur.TID == prev.TID &&
		cur.Tag == prev.Tag
}

func formatLogCount(displayed, total int) string {
	if displayed == total {
		return strconv.Itoa(total) + " logs"
	}
	return strconv.Itoa(displayed) + " / " + strconv.Itoa(total) + " logs"
}
