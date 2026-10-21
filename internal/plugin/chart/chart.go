// Package chart implements a plugin that extracts numeric values from
// logcat lines matching a user pattern and renders a real-time line chart.
package chart

import (
	"fmt"
	"image/color"
	"regexp"
	"strconv"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/rmarkiewicz/android-companion/internal/logcat"
	"github.com/rmarkiewicz/android-companion/internal/plugin"
)

const (
	// maxPoints bounds memory; old points are dropped FIFO.
	maxPoints = 2000
	// maxSegments caps drawn line segments (we downsample beyond this).
	maxSegments = 500
	// chartHeight is the pixel height of the chart area.
	chartHeight = 160
)

// point is one sample: X = log timestamp (unix millis), Y = value.
type point struct {
	t int64
	v float64
}

// Plugin is a line-chart plugin instance.
type Plugin struct {
	name    string
	pattern string
	re      *regexp.Regexp

	mu     sync.RWMutex
	points []point

	// UI
	chartWidget *chartWidget
	label       *widget.Label // shows last value + count
	container   *fyne.Container
	onClose     func()
}

// CompilePattern is kept for backward compatibility; use
// plugin.CompilePattern instead.
func CompilePattern(pattern string) (*regexp.Regexp, error) {
	return plugin.CompilePattern(pattern)
}

// New creates a chart plugin. onClose is called when the user closes it.
func New(name, pattern string, onClose func()) (*Plugin, error) {
	re, err := plugin.CompilePattern(pattern)
	if err != nil {
		return nil, err
	}

	p := &Plugin{
		name:    name,
		pattern: pattern,
		re:      re,
		onClose: onClose,
	}

	p.chartWidget = newChartWidget(p)
	p.label = widget.NewLabel("no data")
	p.label.Alignment = fyne.TextAlignTrailing

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

	body := container.NewBorder(nil, p.label, nil, nil,
		container.NewGridWrap(fyne.NewSize(300, chartHeight), p.chartWidget),
	)

	p.container = container.NewVBox(header, body, widget.NewSeparator())
	return p, nil
}

// Name implements plugin.Plugin.
func (p *Plugin) Name() string { return p.name }

// Pattern returns the user pattern used to create this chart (for persistence).
func (p *Plugin) Pattern() string { return p.pattern }

// Widget implements plugin.Plugin.
func (p *Plugin) Widget() fyne.CanvasObject { return p.container }

// Close implements plugin.Plugin.
func (p *Plugin) Close() {}

// ProcessLine implements plugin.Plugin. Extracts the value if the line
// matches the pattern.
func (p *Plugin) ProcessLine(e logcat.Entry) {
	m := p.re.FindStringSubmatch(e.Message)
	if m == nil {
		m = p.re.FindStringSubmatch(e.Raw)
		if m == nil {
			return
		}
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return
	}
	t := parseLogTime(e.Time)

	p.mu.Lock()
	if len(p.points) >= maxPoints {
		p.points = p.points[1:]
	}
	p.points = append(p.points, point{t: t, v: v})
	n := len(p.points)
	last := p.points[n-1]
	p.mu.Unlock()

	fyne.Do(func() {
		p.label.SetText(fmt.Sprintf("last: %v  (%d pts)", last.v, n))
		p.chartWidget.Refresh()
	})
}

// parseLogTime converts "MM-DD HH:MM:SS.mmm" to unix millis (current year).
func parseLogTime(s string) int64 {
	if s == "" {
		return time.Now().UnixMilli()
	}
	year := time.Now().Year()
	full := fmt.Sprintf("%d-%s", year, s)
	t, err := time.ParseInLocation("2006-01-02 15:04:05.000", full, time.Local)
	if err != nil {
		return time.Now().UnixMilli()
	}
	return t.UnixMilli()
}

// --- chart widget ---

// chartWidget draws the series as canvas.Line segments (efficient:
// downsampled to maxSegments lines).
type chartWidget struct {
	widget.BaseWidget
	p *Plugin

	bg    *canvas.Rectangle
	lines []*canvas.Line
	objs  []fyne.CanvasObject
}

var (
	chartBgColor   = color.NRGBA{R: 24, G: 24, B: 24, A: 255}
	chartLineColor = color.NRGBA{R: 100, G: 180, B: 255, A: 255}
	chartGridColor = color.NRGBA{R: 55, G: 55, B: 55, A: 255}
)

func newChartWidget(p *Plugin) *chartWidget {
	w := &chartWidget{p: p}
	w.bg = canvas.NewRectangle(chartBgColor)
	w.objs = []fyne.CanvasObject{w.bg}
	w.ExtendBaseWidget(w)
	return w
}

// Refresh recomputes the line segments from the current points.
func (w *chartWidget) Refresh() {
	p := w.p
	p.mu.RLock()
	pts := make([]point, len(p.points))
	copy(pts, p.points)
	p.mu.RUnlock()

	size := w.Size()
	W, H := float32(size.Width), float32(size.Height)
	if W < 10 || H < 10 {
		w.BaseWidget.Refresh()
		return
	}

	objs := []fyne.CanvasObject{w.bg}

	if len(pts) >= 2 {
		minV, maxV := pts[0].v, pts[0].v
		for _, pt := range pts {
			if pt.v < minV {
				minV = pt.v
			}
			if pt.v > maxV {
				maxV = pt.v
			}
		}
		t0, t1 := pts[0].t, pts[len(pts)-1].t
		if t1 == t0 {
			t1 = t0 + 1
		}
		if maxV == minV {
			maxV = minV + 1
		}
		pad := (maxV - minV) * 0.05
		minV -= pad
		maxV += pad

		px := func(t int64) float32 {
			return float32(float64(t-t0) / float64(t1-t0) * float64(W-1))
		}
		py := func(v float64) float32 {
			return H - 1 - float32((v-minV)/(maxV-minV)*float64(H-1))
		}

		// Middle grid line
		midY := py((minV + maxV) / 2)
		grid := canvas.NewLine(chartGridColor)
		grid.Position1 = fyne.NewPos(0, midY)
		grid.Position2 = fyne.NewPos(W, midY)
		grid.StrokeWidth = 1
		objs = append(objs, grid)

		// Y-axis labels: max (top), mid, min (bottom) — raw values, no padding
		labelCol := color.NRGBA{R: 140, G: 140, B: 140, A: 255}
		fontSize := theme.TextSize() - 3
		for _, lv := range []struct {
			v float64
			y float32
		}{
			{maxV - pad, 2},
			{(minV + maxV) / 2, midY - fontSize/2},
			{minV + pad, H - fontSize - 2},
		} {
			txt := canvas.NewText(formatValue(lv.v), labelCol)
			txt.TextSize = fontSize
			txt.TextStyle = fyne.TextStyle{Monospace: true}
			txt.Move(fyne.NewPos(4, lv.y))
			objs = append(objs, txt)
		}

		// Downsample points to maxSegments
		step := 1
		if len(pts) > maxSegments {
			step = len(pts) / maxSegments
		}
		var prev fyne.Position
		for i := 0; i < len(pts); i += step {
			pos := fyne.NewPos(px(pts[i].t), py(pts[i].v))
			if i > 0 {
				ln := canvas.NewLine(chartLineColor)
				ln.Position1 = prev
				ln.Position2 = pos
				ln.StrokeWidth = 1.5
				objs = append(objs, ln)
			}
			prev = pos
		}
		// Ensure last point is always drawn
		last := fyne.NewPos(px(pts[len(pts)-1].t), py(pts[len(pts)-1].v))
		if last != prev {
			ln := canvas.NewLine(chartLineColor)
			ln.Position1 = prev
			ln.Position2 = last
			ln.StrokeWidth = 1.5
			objs = append(objs, ln)
		}
	}

	w.objs = objs
	w.BaseWidget.Refresh()
}

// formatValue renders a value compactly for axis labels.
func formatValue(v float64) string {
	if v == float64(int64(v)) {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}

func (w *chartWidget) CreateRenderer() fyne.WidgetRenderer {
	return &chartRenderer{w: w}
}

type chartRenderer struct {
	w *chartWidget
}

func (r *chartRenderer) Layout(size fyne.Size) {
	r.w.bg.Resize(size)
	// Repositioning of line objects is done in Refresh (absolute coords).
}

func (r *chartRenderer) MinSize() fyne.Size {
	return fyne.NewSize(200, chartHeight)
}

func (r *chartRenderer) Refresh() {
	r.w.bg.Refresh()
	for _, o := range r.w.objs {
		o.Refresh()
	}
	canvas.Refresh(r.w)
}

func (r *chartRenderer) Objects() []fyne.CanvasObject {
	return r.w.objs
}

func (r *chartRenderer) Destroy() {}
