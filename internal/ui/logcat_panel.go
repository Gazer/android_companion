package ui

import (
	"math"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/rmarkiewicz/android-companion/internal/logcat"
)

const (
	// maxDisplayedRows caps the rows kept in the table while following the
	// newest logs. Older rows are dropped in chunks of trimChunk.
	maxDisplayedRows = 20000
	trimChunk        = maxDisplayedRows / 10
	// maxDisplayedRowsPaused is the cap while the user reads older logs.
	// Dropping rows shifts the view (compensated by scrolling), so it's
	// deferred until this limit and then done in chunks of trimChunk.
	maxDisplayedRowsPaused = 2 * maxDisplayedRows
	// msgColumnStep rounds up the message column width, so it doesn't
	// change (and refresh the whole table) for every slightly longer line.
	msgColumnStep = 64
)

// LogcatPanel is the right-side panel with package filter, text filter,
// and a log table that scrolls on both axes.
//
// The table (widget.Table) only creates and binds cells for visible rows
// and columns, and its single scroller handles both axes, so trackpad
// gestures work anywhere on it.
//
// New logs are read incrementally from the store on each RefreshDisplay.
// The whole store is only re-filtered when a filter changes or the
// selected package gets a new PID.
//
// All fields are owned by the Fyne UI goroutine.
type LogcatPanel struct {
	widget.BaseWidget

	store *logcat.Store

	// Filters. pkgPIDs accumulates every PID seen for pkg, so logs from
	// previous runs of the app stay visible after it restarts.
	pkg        string
	pkgPIDs    map[int32]struct{}
	filter     textFilter
	mapVersion uint64

	// nextSeq is the store sequence number of the next entry to read.
	nextSeq uint64
	rows    []logRow
	builder *rowBuilder
	metrics *logMetrics
	msgColW float32

	// following is true while the view sticks to the newest log.
	following bool
	vis       visibleRows

	// UI widgets
	packageFilter *PackageFilter
	filterEntry   *widget.Entry
	table         *widget.Table
	bottomBtn     *widget.Button
	listArea      *fyne.Container
	statusLabel   *widget.Label
}

// visibleRows tracks which rows the table shows. widget.Table doesn't
// expose its scroll offset, but it calls UpdateCell for every visible cell
// on a full refresh, and for the cells that come into view on scroll.
type visibleRows struct {
	// measuring is set during a full refresh triggered by measureVisible.
	measuring bool
	// min and max are the first and last visible rows (-1 if unknown).
	min, max int
}

// NewLogcatPanel creates the panel.
func NewLogcatPanel(store *logcat.Store) *LogcatPanel {
	p := &LogcatPanel{
		store:     store,
		following: true,
		vis:       visibleRows{min: -1, max: -1},
	}

	base := fyne.CurrentApp().Settings().Theme()
	p.metrics = newLogMetrics(base.Size(theme.SizeNameText))
	p.builder = newRowBuilder(p.metrics)

	// The last package and text filter are saved, and restored below.
	prefs := fyne.CurrentApp().Preferences()

	p.packageFilter = NewPackageFilter(func(pkg string) {
		prefs.SetString(prefPackage, pkg)
		p.setPackage(pkg)
		p.rebuild()
	})

	p.filterEntry = widget.NewEntry()
	p.filterEntry.SetPlaceHolder(`Filter: words, ~exclude, "a phrase"`)
	p.filterEntry.OnChanged = func(s string) {
		prefs.SetString(prefFilter, s)
		p.filter = parseTextFilter(s)
		p.rebuild()
	}

	p.statusLabel = widget.NewLabel("")

	p.table = widget.NewTable(
		func() (int, int) { return len(p.rows), numLogColumns },
		func() fyne.CanvasObject { return newLogCell(p.metrics) },
		p.updateCell,
	)
	p.table.HideSeparators = true
	tableArea := container.NewThemeOverride(p.table, logTableTheme{base})
	for col, w := range p.metrics.colW {
		p.table.SetColumnWidth(col, w)
	}
	p.setMessageColumnWidth(0)

	p.bottomBtn = widget.NewButtonWithIcon("Jump to bottom", theme.MoveDownIcon(), p.scrollToEnd)
	p.bottomBtn.Importance = widget.HighImportance
	p.bottomBtn.Hide()

	p.listArea = container.NewStack(tableArea,
		container.NewVBox(
			layout.NewSpacer(),
			container.NewHBox(layout.NewSpacer(), p.bottomBtn, layout.NewSpacer()),
		),
	)

	// Restore the last package and filter. SetText runs OnChanged, which
	// parses the filter and loads the (still empty) list.
	if pkg := prefs.String(prefPackage); pkg != "" {
		p.packageFilter.SetSelected(pkg)
		p.setPackage(pkg)
	}
	p.filterEntry.SetText(prefs.String(prefFilter))

	p.updateStatus()
	p.ExtendBaseWidget(p)
	return p
}

// Preference keys of the last package and text filter.
const (
	prefPackage = "logcat.package"
	prefFilter  = "logcat.filter"
)

// setPackage sets the package filter ("" for all packages).
func (p *LogcatPanel) setPackage(pkg string) {
	p.pkg = pkg
	p.pkgPIDs = nil
	if pkg != "" {
		p.pkgPIDs = p.store.PIDsForPackage(pkg)
	}
}

// Content returns the panel's UI tree.
func (p *LogcatPanel) Content() fyne.CanvasObject {
	topBar := container.NewBorder(nil, nil, p.packageFilter.Content(), nil, p.filterEntry)
	header := container.NewVBox(topBar, widget.NewSeparator())
	footer := container.NewHBox(p.statusLabel)

	return container.NewBorder(header, footer, nil, nil, p.listArea)
}

func (p *LogcatPanel) FocusPoint() fyne.Focusable {
	return p.filterEntry
}

// SetWindow wires the parent window for the package filter dialog.
func (p *LogcatPanel) SetWindow(w fyne.Window) {
	p.packageFilter.SetWindow(w)
}

// RefreshDisplay adds the logs that arrived since the last call. Call it
// periodically from the UI goroutine.
func (p *LogcatPanel) RefreshDisplay() {
	if v := p.store.MapVersion(); v != p.mapVersion {
		p.mapVersion = v
		p.refreshPackages()
		// Logs from PIDs that weren't mapped yet (e.g. the app just
		// started) were skipped: re-filter if the package got new PIDs.
		if p.pkg != "" && p.addPackagePIDs(p.store.PIDsForPackage(p.pkg)) {
			p.rebuild()
			return
		}
	}

	entries, next := p.store.Collect(p.nextSeq, p.matches)
	p.nextSeq = next
	if len(entries) > 0 {
		p.appendEntries(entries)
	}
	p.updateStatus()
}

// addPackagePIDs merges pids into pkgPIDs and reports whether any was new.
func (p *LogcatPanel) addPackagePIDs(pids map[int32]struct{}) bool {
	added := false
	for pid := range pids {
		if _, ok := p.pkgPIDs[pid]; !ok {
			p.pkgPIDs[pid] = struct{}{}
			added = true
		}
	}
	return added
}

// matches applies the package and text filters. It runs under the store
// lock (see Store.Collect).
func (p *LogcatPanel) matches(e *logcat.Entry) bool {
	if p.pkg != "" {
		if _, ok := p.pkgPIDs[e.PIDNum()]; !ok {
			return false
		}
	}
	return p.filter.match(e.Raw)
}

// rebuild re-filters the whole store and jumps to the newest log.
func (p *LogcatPanel) rebuild() {
	entries, next := p.store.CollectLast(maxDisplayedRows, p.matches)
	p.nextSeq = next

	clear(p.rows)
	p.rows = p.rows[:0]
	p.builder.reset()
	for i := range entries {
		p.rows = append(p.rows, p.builder.build(&entries[i]))
	}
	p.msgColW = 0
	p.setMessageColumnWidth(p.builder.maxMsgW)

	p.table.Refresh()
	p.scrollToEnd()
	p.updateStatus()
}

// appendEntries adds new rows. If the user was looking at the newest log
// the view follows; otherwise it stays where it is.
func (p *LogcatPanel) appendEntries(entries []logcat.Entry) {
	before := len(p.rows)
	for i := range entries {
		p.rows = append(p.rows, p.builder.build(&entries[i]))
	}
	p.setMessageColumnWidth(p.builder.maxMsgW)

	// Rows were only appended, so the table still shows the same row
	// indices: if the last one was among them, the user is at the bottom.
	p.measureVisible()
	atBottom := p.vis.max < 0 || p.vis.max >= before-1

	if atBottom {
		if len(p.rows) > maxDisplayedRows {
			p.trimFront(len(p.rows) - maxDisplayedRows + trimChunk)
		}
		p.scrollToEnd()
		return
	}

	p.setFollowing(false)
	if len(p.rows) > maxDisplayedRowsPaused {
		dropped := p.trimFront(len(p.rows) - maxDisplayedRowsPaused + trimChunk)
		// Keep the same logs on screen.
		if off, ok := tableOffset(p.table); ok {
			y := max(off.Y-float32(dropped)*p.metrics.rowStride(), 0)
			p.table.ScrollToOffset(fyne.NewPos(off.X, y))
		} else {
			p.table.Refresh()
		}
	}
}

// trimFront drops the oldest n rows and returns how many were dropped.
func (p *LogcatPanel) trimFront(n int) int {
	n = min(n, len(p.rows))
	if n <= 0 {
		return 0
	}
	kept := copy(p.rows, p.rows[n:])
	clear(p.rows[kept:]) // release the strings for the GC
	p.rows = p.rows[:kept]
	if kept > 0 && p.rows[0].parsed {
		p.rows[0].showHeader = true // its header row is gone
	}
	return n
}

// scrollToEnd shows the newest row, keeping the horizontal offset, and
// resumes following.
func (p *LogcatPanel) scrollToEnd() {
	n := len(p.rows)
	if n == 0 {
		p.table.Refresh()
		p.setFollowing(true)
		return
	}
	if off, ok := tableOffset(p.table); ok {
		// The scroller clamps Y to the content height.
		end := fyne.NewPos(off.X, math.MaxFloat32)
		p.table.ScrollToOffset(end)
		// The clamp checks the scroller's previous content size first: if
		// the rows used to fit in the view it resets the offset to the
		// top, so repeat once the scroller has picked up the new size.
		p.table.ScrollToOffset(end)
	} else {
		p.table.ScrollToBottom()
	}

	if p.vis.max >= 0 {
		span := p.vis.max - p.vis.min
		p.vis.max = n - 1
		p.vis.min = max(n-1-span, 0)
	}
	p.setFollowing(true)
}

// measureVisible refreshes the table and records which rows it shows.
func (p *LogcatPanel) measureVisible() {
	p.vis = visibleRows{measuring: true, min: -1, max: -1}
	p.table.Refresh()
	p.vis.measuring = false
}

// noteVisibleRow is called for every bound cell. Outside measureVisible
// it means the row just came into view (scroll or resize).
func (p *LogcatPanel) noteVisibleRow(row int) {
	v := &p.vis
	if v.measuring {
		if v.min < 0 || row < v.min {
			v.min = row
		}
		if row > v.max {
			v.max = row
		}
		return
	}
	if v.max < 0 {
		return
	}
	span := v.max - v.min
	switch {
	case row < v.min:
		v.min, v.max = row, row+span
	case row > v.max:
		v.min, v.max = row-span, row
	default:
		return
	}
	p.setFollowing(v.max >= len(p.rows)-1)
}

// setFollowing records whether the view follows new logs and shows the
// "Jump to bottom" button when it doesn't.
func (p *LogcatPanel) setFollowing(on bool) {
	p.following = on
	if on || len(p.rows) == 0 {
		p.bottomBtn.Hide()
	} else {
		p.bottomBtn.Show()
	}
}

// updateCell binds a table cell to its row (Table.UpdateCell).
func (p *LogcatPanel) updateCell(id widget.TableCellID, obj fyne.CanvasObject) {
	c := obj.(*logCell)
	if id.Row < 0 || id.Row >= len(p.rows) {
		if id.Col == colLevel {
			c.setBadge("", nil)
		} else {
			c.setText("", nil)
		}
		return
	}
	p.noteVisibleRow(id.Row)

	r := &p.rows[id.Row]
	header := r.parsed && r.showHeader
	switch id.Col {
	case colTime:
		c.setText(textIf(header, r.time), metaColorValue)
	case colPID:
		c.setText(textIf(header, r.pidTid), metaColorValue)
	case colTag:
		c.setText(textIf(header, r.tag), r.tagColor)
	case colLevel:
		if header {
			c.setBadge(r.level, r.levelColor)
		} else {
			c.setBadge("", nil)
		}
	default:
		c.setText(r.msg, r.msgColor)
	}
}

func textIf(ok bool, s string) string {
	if ok {
		return s
	}
	return ""
}

// setMessageColumnWidth sizes the message column for text of width textW.
func (p *LogcatPanel) setMessageColumnWidth(textW float32) {
	w := float32(math.Ceil(float64(textW+p.metrics.charW)/msgColumnStep) * msgColumnStep)
	if w == p.msgColW {
		return
	}
	p.msgColW = w
	p.table.SetColumnWidth(colMessage, w)
}

func (p *LogcatPanel) updateStatus() {
	text := "Waiting for logs..."
	if total := p.store.Len(); total > 0 {
		text = formatLogCount(len(p.rows), total)
	}
	if p.statusLabel.Text != text {
		p.statusLabel.SetText(text)
	}
}

// refreshPackages updates the package list from the store's index.
func (p *LogcatPanel) refreshPackages() {
	p.packageFilter.SetPackages(p.store.Packages())
}

// SetAutoScroll toggles following the newest logs.
func (p *LogcatPanel) SetAutoScroll(b bool) {
	if b {
		p.scrollToEnd()
	} else {
		p.setFollowing(false)
	}
}

// Clear wipes the display (not the store).
func (p *LogcatPanel) Clear() {
	_, p.nextSeq = p.store.Collect(math.MaxUint64, nil)
	clear(p.rows)
	p.rows = p.rows[:0]
	p.builder.reset()
	p.scrollToEnd()
	p.updateStatus()
}

// ResetDevice reloads the panel after the store switched to another device
// (and was cleared). The package filter is kept, but the PIDs collected
// for it belonged to the previous device.
func (p *LogcatPanel) ResetDevice() {
	p.setPackage(p.pkg)
	p.mapVersion = p.store.MapVersion()
	p.refreshPackages()
	p.rebuild()
}

// CreateRenderer is required by fyne.Widget.
func (p *LogcatPanel) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.Content())
}

func formatLogCount(displayed, total int) string {
	if displayed == total {
		return strconv.Itoa(total) + " logs"
	}
	return strconv.Itoa(displayed) + " / " + strconv.Itoa(total) + " logs"
}
