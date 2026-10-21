// Package status implements a plugin that shows a colored state badge
// based on which logcat line matched last. Each rule maps a substring
// pattern to a label and a color; the most recent matching rule wins.
package status

import (
	"fmt"
	"image/color"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/rmarkiewicz/android-companion/internal/logcat"
	"github.com/rmarkiewicz/android-companion/internal/plugin"
)

// Rule is one state mapping: when Pattern matches a log line, the state
// becomes Label shown in Color.
type Rule struct {
	Pattern string `json:"pattern"`
	Label   string `json:"label"`
	Color   string `json:"color"` // hex like "#4CAF50"
}

// Plugin is a status-badge plugin instance.
type Plugin struct {
	name  string
	rules []Rule
	res   []*regexp.Regexp // compiled patterns, parallel to rules

	mu     sync.RWMutex
	active int    // index of the currently active rule, -1 = none
	captures []string // capture groups from the line that set the state

	// UI
	badge     *canvas.Rectangle
	label     *canvas.Text
	container *fyne.Container
	onClose   func()
}

// New creates a status plugin. rules are evaluated in order; the last
// matching rule in the stream defines the current state.
//
// Patterns support {%d} / {%f} capture placeholders (same syntax as the
// chart plugin). Captured values can be interpolated into the label with
// $0, $1, ... where $N is the Nth placeholder of the matching rule.
func New(name string, rules []Rule, onClose func()) (*Plugin, error) {
	if len(rules) == 0 {
		return nil, fmt.Errorf("at least one rule is required")
	}
	res := make([]*regexp.Regexp, len(rules))
	for i, r := range rules {
		if r.Pattern == "" || r.Label == "" {
			return nil, fmt.Errorf("rule %d: pattern and label are required", i+1)
		}
		if _, err := ParseHexColor(r.Color); err != nil {
			return nil, fmt.Errorf("rule %d: %v", i+1, err)
		}
		re, err := plugin.CompilePattern(r.Pattern)
		if err != nil {
			return nil, fmt.Errorf("rule %d: %v", i+1, err)
		}
		res[i] = re
	}

	p := &Plugin{
		name:    name,
		rules:   rules,
		res:     res,
		active:  -1,
		onClose: onClose,
	}

	p.badge = canvas.NewRectangle(color.NRGBA{R: 60, G: 60, B: 60, A: 255})
	p.badge.CornerRadius = 6
	p.label = canvas.NewText("(no state)", color.NRGBA{R: 140, G: 140, B: 140, A: 255})
	p.label.TextStyle = fyne.TextStyle{Bold: true}
	p.label.TextSize = theme.TextSize() + 4
	p.label.Alignment = fyne.TextAlignCenter

	closeBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		if p.onClose != nil {
			p.onClose()
		}
	})
	closeBtn.Importance = widget.LowImportance

	header := container.NewBorder(nil, nil,
		widget.NewLabelWithStyle(name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		closeBtn,
	)

	badgeBox := container.NewGridWrap(fyne.NewSize(300, 48),
		container.NewStack(p.badge, container.NewCenter(p.label)),
	)

	p.container = container.NewVBox(header, badgeBox, widget.NewSeparator())
	return p, nil
}

// Name implements plugin.Plugin.
func (p *Plugin) Name() string { return p.name }

// Rules returns the configured rules (for persistence).
func (p *Plugin) Rules() []Rule {
	cp := make([]Rule, len(p.rules))
	copy(cp, p.rules)
	return cp
}

// Widget implements plugin.Plugin.
func (p *Plugin) Widget() fyne.CanvasObject { return p.container }

// Close implements plugin.Plugin.
func (p *Plugin) Close() {}

// ProcessLine implements plugin.Plugin. The last matching rule in the
// stream defines the current state; its captures interpolate into the label.
func (p *Plugin) ProcessLine(e logcat.Entry) {
	text := e.Message
	if text == "" {
		text = e.Raw
	}

	p.mu.Lock()
	changed := false
	for i, re := range p.res {
		if m := re.FindStringSubmatch(text); m != nil {
			if p.active != i || !equalStrings(p.captures, m[1:]) {
				p.active = i
				p.captures = m[1:]
				changed = true
			}
		}
	}
	active := p.active
	captures := p.captures
	p.mu.Unlock()

	if !changed {
		return
	}

	rule := p.rules[active]
	col, err := ParseHexColor(rule.Color)
	if err != nil {
		col = color.NRGBA{R: 128, G: 128, B: 128, A: 255}
	}
	label := Interpolate(rule.Label, captures)

	fyne.Do(func() {
		p.badge.FillColor = col
		p.badge.Refresh()
		p.label.Text = label
		p.label.Color = contrastColor(col)
		p.label.Refresh()
	})
}

// Interpolate replaces $0, $1, ... in label with the captured values.
func Interpolate(label string, captures []string) string {
	return labelRefRe.ReplaceAllStringFunc(label, func(ref string) string {
		n, err := strconv.Atoi(ref[1:])
		if err != nil || n < 0 || n >= len(captures) {
			return ref // leave unknown refs as-is
		}
		return captures[n]
	})
}

var labelRefRe = regexp.MustCompile(`\$(\d+)`)

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ParseHexColor parses "#RGB", "#RRGGBB" or "#RRGGBBAA".
func ParseHexColor(s string) (color.Color, error) {
	s = strings.TrimPrefix(s, "#")
	var r, g, b, a uint8 = 0, 0, 0, 255
	switch len(s) {
	case 3:
		if _, err := fmt.Sscanf(s, "%1x%1x%1x", &r, &g, &b); err != nil {
			return nil, fmt.Errorf("invalid color %q", s)
		}
		r *= 17
		g *= 17
		b *= 17
	case 6:
		var ri, gi, bi uint32
		if _, err := fmt.Sscanf(s, "%02x%02x%02x", &ri, &gi, &bi); err != nil {
			return nil, fmt.Errorf("invalid color %q", s)
		}
		r, g, b = uint8(ri), uint8(gi), uint8(bi)
	case 8:
		var ri, gi, bi, ai uint32
		if _, err := fmt.Sscanf(s, "%02x%02x%02x%02x", &ri, &gi, &bi, &ai); err != nil {
			return nil, fmt.Errorf("invalid color %q", s)
		}
		r, g, b, a = uint8(ri), uint8(gi), uint8(bi), uint8(ai)
	default:
		return nil, fmt.Errorf("invalid color %q (use #RRGGBB)", s)
	}
	return color.NRGBA{R: r, G: g, B: b, A: a}, nil
}

// contrastColor picks black or white text for the given background.
func contrastColor(bg color.Color) color.Color {
	r, g, b, _ := bg.RGBA()
	// Relative luminance (simplified)
	lum := 0.299*float64(r)/257 + 0.587*float64(g)/257 + 0.114*float64(b)/257
	if lum > 128 {
		return color.NRGBA{A: 255} // black
	}
	return color.NRGBA{R: 255, G: 255, B: 255, A: 255} // white
}
