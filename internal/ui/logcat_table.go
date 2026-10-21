package ui

import (
	"image/color"
	"math"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/rmarkiewicz/android-companion/internal/logcat"
)

// Table columns.
const (
	colTime = iota
	colPID
	colTag
	colLevel
	colMessage
	numLogColumns
)

const (
	// tagMaxChars is the tag column width in characters (Android Studio
	// style: longer tags are truncated in the middle).
	tagMaxChars = 22
	// logTablePadding replaces the theme padding inside the table. The
	// table uses it as the gap between rows and columns, so a small value
	// keeps rows dense (row height = text height + padding).
	logTablePadding = 2
	// logColumnGap is added to each fixed column, on top of the padding.
	logColumnGap = 6
)

var currentYear = time.Now().Format("2006")

// badgeTextStyle is the level letter style. Not monospace: the built-in
// themes have no bold monospace font, so Bold would be ignored.
var badgeTextStyle = fyne.TextStyle{Bold: true}

// logMetrics holds the pixel sizes derived from the monospace font.
type logMetrics struct {
	textSize      float32
	badgeTextSize float32
	charW         float32 // advance of one monospace character
	cellH         float32
	badgeW        float32
	colW          [colMessage]float32 // widths of the fixed-size columns
}

func newLogMetrics(textSize float32) *logMetrics {
	mono := fyne.TextStyle{Monospace: true}
	measure := func(s string) float32 { return fyne.MeasureText(s, textSize, mono).Width }

	m := &logMetrics{textSize: textSize, badgeTextSize: textSize - 2}
	one := fyne.MeasureText("M", textSize, mono)
	m.charW = one.Width
	m.cellH = float32(math.Ceil(float64(one.Height)))
	m.badgeW = float32(math.Ceil(float64(
		fyne.MeasureText("W", m.badgeTextSize, badgeTextStyle).Width + 8)))

	// Columns are sized from sample strings so text never spills into the
	// next column (table cells are not clipped).
	m.colW[colTime] = measure("0000-00-00 00:00:00.000") + logColumnGap
	m.colW[colPID] = measure("000000-000000") + logColumnGap
	m.colW[colTag] = measure(strings.Repeat("M", tagMaxChars)) + logColumnGap
	m.colW[colLevel] = m.badgeW + logColumnGap
	return m
}

// rowStride is the distance between the tops of two consecutive rows.
func (m *logMetrics) rowStride() float32 { return m.cellH + logTablePadding }

// textWidth returns the rendered width of s in the monospace font. Plain
// ASCII (the common case) is computed without shaping the text.
func (m *logMetrics) textWidth(s string) float32 {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= utf8.RuneSelf || c == '\t' {
			return fyne.MeasureText(s, m.textSize, fyne.TextStyle{Monospace: true}).Width
		}
	}
	return float32(len(s)) * m.charW
}

// logRow is a display-ready log line. Strings and colors are computed
// once when the line is added, so binding a cell never allocates.
type logRow struct {
	time, pidTid, tag, level, msg  string
	tagColor, levelColor, msgColor color.Color
	// parsed is false for raw lines without a logcat header.
	parsed bool
	// showHeader is false for lines that repeat the previous line's header
	// (multi-line messages): Android Studio only draws it once.
	showHeader bool
}

type rowHeader struct{ time, pid, tid, tag string }

// rowBuilder turns store entries into logRows, tracking the state needed
// across consecutive rows.
type rowBuilder struct {
	m         *logMetrics
	tagColors map[string]color.Color
	last      rowHeader
	hasLast   bool
	// maxMsgW is the widest message seen since the last reset.
	maxMsgW float32
}

func newRowBuilder(m *logMetrics) *rowBuilder {
	return &rowBuilder{m: m, tagColors: make(map[string]color.Color)}
}

func (b *rowBuilder) reset() {
	b.hasLast = false
	b.maxMsgW = 0
}

func (b *rowBuilder) build(e *logcat.Entry) logRow {
	r := logRow{level: e.Level, msg: e.Message, msgColor: levelColor(e.Level)}
	if r.msg == "" && e.Time == "" {
		r.msg = e.Raw
	}
	if w := b.m.textWidth(r.msg); w > b.maxMsgW {
		b.maxMsgW = w
	}

	if e.Time == "" {
		b.hasLast = false
		return r
	}

	r.parsed = true
	r.time = currentYear + "-" + e.Time
	r.pidTid = e.PID
	if e.TID != "" && e.TID != e.PID {
		r.pidTid = e.PID + "-" + e.TID
	}
	r.tag = truncateMiddle(e.Tag, tagMaxChars)
	r.tagColor = b.tagColor(e.Tag)
	r.levelColor = levelColor(e.Level)

	h := rowHeader{e.Time, e.PID, e.TID, e.Tag}
	r.showHeader = !b.hasLast || h != b.last
	b.last, b.hasLast = h, true
	return r
}

func (b *rowBuilder) tagColor(tag string) color.Color {
	if c, ok := b.tagColors[tag]; ok {
		return c
	}
	if len(b.tagColors) > 4096 {
		clear(b.tagColors)
	}
	c := color.Color(ColorForTag(tag))
	b.tagColors[tag] = c
	return c
}

// logCell is the reusable table cell: a text, plus a rounded background
// when it shows the level badge. The table recycles cells across columns,
// so the mode is set on every bind.
type logCell struct {
	widget.BaseWidget

	m     *logMetrics
	bg    *canvas.Rectangle
	text  *canvas.Text
	badge bool
}

func newLogCell(m *logMetrics) *logCell {
	c := &logCell{m: m}
	c.bg = canvas.NewRectangle(color.Transparent)
	c.bg.CornerRadius = 3
	c.bg.Hide()
	c.text = canvas.NewText("", defaultTextColorValue)
	c.text.TextStyle = fyne.TextStyle{Monospace: true}
	c.text.TextSize = m.textSize
	c.ExtendBaseWidget(c)
	return c
}

// Tapped swallows taps. Otherwise the table would select the cell and
// scroll it fully into view, which for the (very wide) message column
// means jumping to the end of the longest line.
func (c *logCell) Tapped(*fyne.PointEvent) {}

// setText shows s as plain text. The text is only refreshed (which
// re-rasterizes it) when it actually changes.
func (c *logCell) setText(s string, col color.Color) {
	c.setBadgeMode(false)
	if c.text.Text == s && (s == "" || c.text.Color == col) {
		return
	}
	c.text.Text = s
	c.text.Color = col
	c.text.Refresh()
}

// setBadge shows the level letter on a colored background. An empty
// level shows nothing.
func (c *logCell) setBadge(level string, bg color.Color) {
	c.setBadgeMode(true)
	if level == "" {
		if c.bg.Visible() {
			c.bg.Hide()
			c.bg.Refresh()
		}
	} else if !c.bg.Visible() || c.bg.FillColor != bg {
		c.bg.FillColor = bg
		c.bg.Show()
		c.bg.Refresh()
	}
	if c.text.Text != level {
		c.text.Text = level
		c.text.Refresh()
	}
}

func (c *logCell) setBadgeMode(on bool) {
	if c.badge == on {
		return
	}
	c.badge = on
	if on {
		c.text.TextStyle = badgeTextStyle
		c.text.TextSize = c.m.badgeTextSize
		c.text.Alignment = fyne.TextAlignCenter
		c.text.Color = badgeTextColor
	} else {
		c.text.TextStyle = fyne.TextStyle{Monospace: true}
		c.text.TextSize = c.m.textSize
		c.text.Alignment = fyne.TextAlignLeading
		c.bg.Hide()
		c.bg.Refresh()
	}
	c.layout(c.Size())
	c.text.Refresh()
}

func (c *logCell) layout(size fyne.Size) {
	if c.badge {
		badge := fyne.NewSize(c.m.badgeW, size.Height)
		c.bg.Move(fyne.NewPos(0, 0))
		c.bg.Resize(badge)
		c.text.Move(fyne.NewPos(0, 0))
		c.text.Resize(badge)
		return
	}
	c.text.Move(fyne.NewPos(0, 0))
	c.text.Resize(size)
}

func (c *logCell) CreateRenderer() fyne.WidgetRenderer {
	return &logCellRenderer{c: c, objects: []fyne.CanvasObject{c.bg, c.text}}
}

type logCellRenderer struct {
	c       *logCell
	objects []fyne.CanvasObject
}

func (r *logCellRenderer) Layout(size fyne.Size) { r.c.layout(size) }

func (r *logCellRenderer) MinSize() fyne.Size { return fyne.NewSize(r.c.m.charW, r.c.m.cellH) }

func (r *logCellRenderer) Refresh() {
	r.c.layout(r.c.Size())
	r.c.bg.Refresh()
	r.c.text.Refresh()
}

func (r *logCellRenderer) Objects() []fyne.CanvasObject { return r.objects }

func (r *logCellRenderer) Destroy() {}

// logTableTheme shrinks the padding inside the log table (see
// logTablePadding).
type logTableTheme struct{ fyne.Theme }

func (t logTableTheme) Size(n fyne.ThemeSizeName) float32 {
	if n == theme.SizeNamePadding {
		return logTablePadding
	}
	return t.Theme.Size(n)
}

// tableOffset returns the scroll offset of t. widget.Table doesn't expose
// it (only setters like ScrollToOffset), so it's read with reflection.
// ok is false if that stops working after a Fyne upgrade; TestTableOffset
// catches it.
func tableOffset(t *widget.Table) (pos fyne.Position, ok bool) {
	f := reflect.ValueOf(t).Elem().FieldByName("offset")
	if !f.IsValid() || f.Type() != reflect.TypeOf(fyne.Position{}) {
		return fyne.Position{}, false
	}
	return fyne.NewPos(float32(f.FieldByName("X").Float()), float32(f.FieldByName("Y").Float())), true
}

// truncateMiddle keeps head...tail, like Android Studio's tag column.
func truncateMiddle(s string, maxChars int) string {
	if len(s) <= maxChars || utf8.RuneCountInString(s) <= maxChars {
		return s
	}
	r := []rune(s)
	if maxChars < 6 {
		return string(r[:maxChars])
	}
	head := (maxChars - 3) / 2
	tail := maxChars - 3 - head
	return string(r[:head]) + "..." + string(r[len(r)-tail:])
}

// containsFold reports whether s contains lowerSub, ignoring case.
// lowerSub must already be lower case. ASCII needles (the common case)
// are matched without allocating.
func containsFold(s, lowerSub string) bool {
	if lowerSub == "" {
		return true
	}
	for i := 0; i < len(lowerSub); i++ {
		if lowerSub[i] >= utf8.RuneSelf {
			return strings.Contains(strings.ToLower(s), lowerSub)
		}
	}
	n := len(lowerSub)
	first := lowerSub[0]
	for i := 0; i+n <= len(s); i++ {
		if lowerASCII(s[i]) != first {
			continue
		}
		j := 1
		for j < n && lowerASCII(s[i+j]) == lowerSub[j] {
			j++
		}
		if j == n {
			return true
		}
	}
	return false
}

func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}
