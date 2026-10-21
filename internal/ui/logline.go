package ui

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/rmarkiewicz/android-companion/internal/logcat"
)

// Column widths in pixels (monospace font, tuned to Android Studio's look).
const (
	colDateTimeW = 185 // "2026-10-21 11:42:54.331"
	colPidTidW   = 80  // "3151-3782"
	colTagW      = 170 // "CSLIB|Sess...ayReporter"
	colLevelW    = 20  // badge
	colGap       = 8
)

var currentYear = time.Now().Format("2006")

// fixedColumnsWidth is the total pixel width of all columns before the
// message column (exported for the panel's width calculation).
const FixedColumnsWidth = colDateTimeW + colPidTidW + colTagW + colLevelW + 4*colGap

// columnsLayout positions children at fixed x offsets with fixed widths.
// Children: [time, pidTid, tag, levelBadge, message]
type columnsLayout struct{}

func (c *columnsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) != 5 {
		return
	}
	x := float32(0)

	objects[0].Move(fyne.NewPos(x, 0))
	objects[0].Resize(fyne.NewSize(colDateTimeW, size.Height))
	x += colDateTimeW + colGap

	objects[1].Move(fyne.NewPos(x, 0))
	objects[1].Resize(fyne.NewSize(colPidTidW, size.Height))
	x += colPidTidW + colGap

	objects[2].Move(fyne.NewPos(x, 0))
	objects[2].Resize(fyne.NewSize(colTagW, size.Height))
	x += colTagW + colGap

	objects[3].Move(fyne.NewPos(x, 0))
	objects[3].Resize(fyne.NewSize(colLevelW, size.Height))
	x += colLevelW + colGap

	msgW := size.Width - x
	if msgW < 0 {
		msgW = 0
	}
	objects[4].Move(fyne.NewPos(x, 0))
	objects[4].Resize(fyne.NewSize(msgW, size.Height))
}

func (c *columnsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) != 5 {
		return fyne.NewSize(0, 0)
	}
	// Message column is as wide as the message text itself: this lets an
	// outer horizontal scroller reveal long lines instead of truncating.
	msgMin := objects[4].MinSize()
	w := colDateTimeW + colPidTidW + colTagW + colLevelW + 4*colGap + msgMin.Width
	return fyne.NewSize(w, msgMin.Height)
}

// levelBadge is a small colored rectangle with a single letter.
type levelBadge struct {
	widget.BaseWidget
	bg    *canvas.Rectangle
	label *canvas.Text
}

func newLevelBadge() *levelBadge {
	b := &levelBadge{}
	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = 3
	b.label = canvas.NewText("", color.White)
	b.label.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
	b.label.TextSize = theme.TextSize() - 2
	b.label.Alignment = fyne.TextAlignCenter
	b.ExtendBaseWidget(b)
	return b
}

// SetLevel sets the badge letter/color. Empty level hides the badge
// (used for continuation lines of multi-line messages).
func (b *levelBadge) SetLevel(level string) {
	if level == "" {
		b.bg.FillColor = color.Transparent
		b.label.Text = ""
	} else {
		b.bg.FillColor = ColorForLevel(level)
		b.label.Text = level
	}
	b.Refresh()
}

func (b *levelBadge) CreateRenderer() fyne.WidgetRenderer {
	return &badgeRenderer{b: b, objects: []fyne.CanvasObject{b.bg, b.label}}
}

type badgeRenderer struct {
	b       *levelBadge
	objects []fyne.CanvasObject
}

func (r *badgeRenderer) Layout(size fyne.Size) {
	r.b.bg.Resize(size)
	r.b.label.Resize(size)
}
func (r *badgeRenderer) MinSize() fyne.Size {
	return fyne.NewSize(colLevelW, theme.TextSize()+6)
}
func (r *badgeRenderer) Refresh() {
	r.b.bg.Refresh()
	r.b.label.Refresh()
}
func (r *badgeRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *badgeRenderer) Destroy()                     {}

// LogLine renders one logcat row with fixed columns, Android Studio style.
type LogLine struct {
	widget.BaseWidget

	timeText *canvas.Text
	pidText  *canvas.Text
	tagText  *canvas.Text
	badge    *levelBadge
	msgText  *canvas.Text

	row *fyne.Container
}

// NewLogLine creates a reusable log line widget for the List.
func NewLogLine() *LogLine {
	l := &LogLine{}

	fontSize := theme.TextSize()
	mono := fyne.TextStyle{Monospace: true}

	l.timeText = canvas.NewText("", MetaColor())
	l.timeText.TextStyle = mono
	l.timeText.TextSize = fontSize

	l.pidText = canvas.NewText("", MetaColor())
	l.pidText.TextStyle = mono
	l.pidText.TextSize = fontSize

	l.tagText = canvas.NewText("", MetaColor())
	l.tagText.TextStyle = mono
	l.tagText.TextSize = fontSize

	l.badge = newLevelBadge()

	l.msgText = canvas.NewText("", DefaultTextColor())
	l.msgText.TextStyle = mono
	l.msgText.TextSize = fontSize

	l.row = container.New(&columnsLayout{},
		l.timeText, l.pidText, l.tagText, l.badge, l.msgText,
	)

	l.ExtendBaseWidget(l)
	return l
}

// SetEntry updates the row's content and colors.
//
// Two modes, matching Android Studio:
//   - Full row (continuation=false): all columns filled, badge shows level.
//   - Continuation row (continuation=true): left columns and badge are
//     empty, the message sits under the message column, colored with the
//     entry's level. Android Studio collapses repeated headers of
//     multi-line log messages this way.
func (l *LogLine) SetEntry(e logcat.Entry, continuation bool) {
	if e.Time == "" {
		// Unparsed line: no header at all
		continuation = true
	}

	if !continuation {
		l.timeText.Text = currentYear + "-" + e.Time
		l.timeText.Color = MetaColor()

		pidTid := e.PID
		if e.TID != "" && e.TID != e.PID {
			pidTid = e.PID + "-" + e.TID
		}
		l.pidText.Text = pidTid
		l.pidText.Color = MetaColor()

		l.tagText.Text = truncateMiddle(e.Tag, 22)
		l.tagText.Color = ColorForTag(e.Tag)

		l.badge.SetLevel(e.Level)
	} else {
		l.timeText.Text = ""
		l.pidText.Text = ""
		l.tagText.Text = ""
		l.badge.SetLevel("")
	}

	msg := e.Message
	if msg == "" && e.Time == "" {
		msg = e.Raw
	}
	l.msgText.Text = msg
	if e.Level != "" {
		l.msgText.Color = ColorForLevel(e.Level)
	} else {
		l.msgText.Color = DefaultTextColor()
	}
	l.Refresh()
}

// CreateRenderer implements fyne.Widget.
func (l *LogLine) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(l.row)
}

// MinSize implements fyne.Widget.
func (l *LogLine) MinSize() fyne.Size {
	return l.row.MinSize()
}

// Refresh propagates to children.
func (l *LogLine) Refresh() {
	l.timeText.Refresh()
	l.pidText.Refresh()
	l.tagText.Refresh()
	l.badge.Refresh()
	l.msgText.Refresh()
	l.BaseWidget.Refresh()
}

// truncateMiddle keeps head...tail, like Android Studio's tag column.
func truncateMiddle(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	if maxChars < 6 {
		return s[:maxChars]
	}
	head := (maxChars - 3) / 2
	tail := maxChars - 3 - head
	return s[:head] + "..." + s[len(s)-tail:]
}
