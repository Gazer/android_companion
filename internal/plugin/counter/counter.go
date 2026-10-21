// Package counter implements a plugin that counts the logcat lines that
// match a pattern, with a button to reset the count.
package counter

import (
	"errors"
	"regexp"
	"strconv"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/rmarkiewicz/android-companion/internal/logcat"
	"github.com/rmarkiewicz/android-companion/internal/plugin"
)

// refreshDelay throttles UI updates: a chatty log can match thousands of
// lines per second, but the count is shown at most every refreshDelay.
const refreshDelay = 100 * time.Millisecond

// Plugin is a counter plugin instance.
type Plugin struct {
	name    string
	pattern string
	re      *regexp.Regexp

	count          atomic.Int64
	refreshPending atomic.Bool
	// schedule runs f on the UI goroutine after refreshDelay (replaced in
	// tests).
	schedule func(f func())

	// UI
	countText *canvas.Text
	resetBtn  *widget.Button
	container *fyne.Container
	onClose   func()
	onEdit    func()
}

// New creates a counter of the lines matching pattern (same syntax as the
// other plugins: literal text, with optional {%d} {%f} {%s} placeholders).
func New(name, pattern string, onClose func()) (*Plugin, error) {
	if pattern == "" {
		return nil, errors.New("the pattern is required")
	}
	re, err := plugin.CompilePattern(pattern)
	if err != nil {
		return nil, err
	}

	p := &Plugin{name: name, pattern: pattern, re: re, onClose: onClose}
	p.schedule = func(f func()) {
		time.AfterFunc(refreshDelay, func() { fyne.Do(f) })
	}

	p.countText = canvas.NewText("0", theme.Color(theme.ColorNameForeground))
	p.countText.TextStyle = fyne.TextStyle{Bold: true}
	p.countText.TextSize = theme.TextSize() * 2

	patternLabel := widget.NewLabel(pattern)
	patternLabel.SizeName = theme.SizeNameCaptionText
	patternLabel.Truncation = fyne.TextTruncateEllipsis

	p.resetBtn = widget.NewButtonWithIcon("Reset", theme.ViewRefreshIcon(), p.Reset)

	closeBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		if p.onClose != nil {
			p.onClose()
		}
	})
	closeBtn.Importance = widget.LowImportance
	editBtn := widget.NewButtonWithIcon("", theme.SettingsIcon(), func() {
		if p.onEdit != nil {
			p.onEdit()
		}
	})
	editBtn.Importance = widget.LowImportance

	header := container.NewBorder(nil, nil,
		widget.NewLabelWithStyle(name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewHBox(editBtn, closeBtn),
	)
	body := container.NewBorder(nil, nil, nil, container.NewCenter(p.resetBtn),
		container.NewVBox(container.NewPadded(p.countText), patternLabel),
	)

	p.container = container.NewVBox(header, body, widget.NewSeparator())
	return p, nil
}

// Name implements plugin.Plugin.
func (p *Plugin) Name() string { return p.name }

// Pattern returns the pattern (for persistence).
func (p *Plugin) Pattern() string { return p.pattern }

// Count returns the number of matching lines since the last reset.
func (p *Plugin) Count() int64 { return p.count.Load() }

// SetCount sets the count (e.g. to keep it when the counter is edited).
// Call it from the UI goroutine.
func (p *Plugin) SetCount(n int64) {
	p.count.Store(n)
	p.showCount()
}

// Reset sets the count back to 0. Call it from the UI goroutine.
func (p *Plugin) Reset() { p.SetCount(0) }

// SetOnEdit sets the callback for the edit button.
func (p *Plugin) SetOnEdit(f func()) { p.onEdit = f }

// SetOnClose sets the callback for the close button.
func (p *Plugin) SetOnClose(f func()) { p.onClose = f }

// Widget implements plugin.Plugin.
func (p *Plugin) Widget() fyne.CanvasObject { return p.container }

// Close implements plugin.Plugin.
func (p *Plugin) Close() {}

// ProcessLine implements plugin.Plugin: counts the line if it matches. The
// whole logcat line is searched (time, PID, tag and message).
func (p *Plugin) ProcessLine(e logcat.Entry) {
	line := e.Raw
	if line == "" {
		line = e.Message
	}
	if !p.re.MatchString(line) {
		return
	}
	p.count.Add(1)
	if p.refreshPending.CompareAndSwap(false, true) {
		p.schedule(func() {
			// Cleared before reading: matches after this point schedule
			// a new refresh.
			p.refreshPending.Store(false)
			p.showCount()
		})
	}
}

// showCount updates the label. UI goroutine only.
func (p *Plugin) showCount() {
	text := formatCount(p.count.Load())
	if p.countText.Text != text {
		p.countText.Text = text
		p.countText.Refresh()
	}
}

// formatCount adds thousands separators: 1234567 -> "1,234,567".
func formatCount(n int64) string {
	s := strconv.FormatInt(n, 10)
	if n < 0 || len(s) <= 3 {
		return s
	}
	out := make([]byte, 0, len(s)+len(s)/3)
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
