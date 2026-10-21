package ui

import (
	"fmt"
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
	// maxDisplayedEntries caps how many items the list handles.
	maxDisplayedEntries = 1000
	// logRowHeight is the fixed pixel height of one log line.
	logRowHeight = 20
)

// LogcatPanel is the right-side panel with package filter, text filter,
// and a scrollable (both axes) virtualized log list.
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
	vlist         *vList
	scroll        *container.Scroll
	bottomBtn     *widget.Button
	listArea      *fyne.Container
	statusLabel   *widget.Label

	autoScroll bool
	// lastOffsetY tracks the scroll offset to detect user scrolls
	lastOffsetY float32

	// Track last state to avoid unnecessary rebuilds
	lastCount   int
	lastFilter  string
	lastPackage string
	lastFullW   float32
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

	// Virtualized list inside a bidirectional Scroll: one scroller handles
	// both axes, so trackpad horizontal gestures work everywhere.
	p.vlist = newVList(logRowHeight,
		func() int {
			p.mu.RLock()
			defer p.mu.RUnlock()
			return len(p.displayed)
		},
		func() fyne.CanvasObject {
			return NewLogLine()
		},
		func(i int, obj fyne.CanvasObject) {
			p.mu.RLock()
			defer p.mu.RUnlock()
			if i < 0 || i >= len(p.displayed) {
				return
			}
			line := obj.(*LogLine)
			line.SetEntry(p.displayed[i], isContinuation(p.displayed, i))
		},
	)

	p.scroll = container.NewScroll(p.vlist)
	p.scroll.Direction = container.ScrollBoth
	// Re-render visible rows on any scroll (virtualization)
	p.scroll.OnScrolled = func(pos fyne.Position) {
		p.vlist.OnScrolled(pos)
		p.checkUserScroll(pos)
	}

	p.bottomBtn = widget.NewButtonWithIcon("Jump to bottom", theme.MoveDownIcon(), func() {
		p.mu.Lock()
		p.autoScroll = true
		p.mu.Unlock()
		p.bottomBtn.Hide()
		p.scroll.ScrollToBottom()
		p.syncScroll()
	})
	p.bottomBtn.Importance = widget.HighImportance
	p.bottomBtn.Hide()

	p.listArea = container.NewStack(p.scroll,
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

// checkUserScroll pauses auto-scroll when the user scrolls up.
func (p *LogcatPanel) checkUserScroll(pos fyne.Position) {
	if pos.Y < p.lastOffsetY-1 {
		p.mu.Lock()
		was := p.autoScroll
		p.autoScroll = false
		p.mu.Unlock()
		if was {
			p.bottomBtn.Show()
		}
	}
	p.lastOffsetY = pos.Y
}

// syncScroll records the current offset after a programmatic scroll.
func (p *LogcatPanel) syncScroll() {
	p.lastOffsetY = p.scroll.Offset.Y
}

// RefreshDisplay re-applies filters and updates the list. Call from UI thread.
func (p *LogcatPanel) RefreshDisplay() {
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

		p.vlist.RefreshRows()
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
	displayed := p.displayed
	p.mu.Unlock()

	// Full content width = fixed columns + longest message (monospace).
	fullW := FixedColumnsWidth + maxMessageWidth(displayed)
	if fullW != p.lastFullW {
		fmt.Printf("[logcat] content %.0f vs visible %.0f\n", fullW, p.scroll.Size().Width)
		p.lastFullW = fullW
	}
	p.vlist.setContentWidth(fullW)

	p.vlist.RefreshRows()
	p.statusLabel.SetText(formatLogCount(count, total))
	if p.autoScroll && count > 0 {
		p.scroll.ScrollToBottom()
		p.syncScroll()
	}
}

// maxMessageWidth returns the pixel width of the longest message.
func maxMessageWidth(entries []logcat.Entry) float32 {
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
	p.vlist.RefreshRows()
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
