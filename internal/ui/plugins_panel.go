package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/rmarkiewicz/android-companion/internal/plugin"
	"github.com/rmarkiewicz/android-companion/internal/plugin/chart"
)

// PluginsPanel is the right-side column holding active plugin widgets,
// with an "Add chart" button and per-widget close buttons.
type PluginsPanel struct {
	widget.BaseWidget

	registry *plugin.Registry
	window   fyne.Window

	vbox    *fyne.Container
	scroll  *container.Scroll
	empty   *widget.Label
	content *fyne.Container
}

// NewPluginsPanel creates the panel wired to the plugin registry.
func NewPluginsPanel(reg *plugin.Registry) *PluginsPanel {
	p := &PluginsPanel{registry: reg}

	p.empty = widget.NewLabel("No widgets yet.\nClick + to add a chart.")
	p.empty.Alignment = fyne.TextAlignCenter
	p.empty.Wrapping = fyne.TextWrapWord

	p.vbox = container.NewVBox(p.empty)
	p.scroll = container.NewVScroll(p.vbox)

	addBtn := widget.NewButtonWithIcon("Add chart", theme.ContentAddIcon(), p.showAddChartDialog)

	p.content = container.NewBorder(
		container.NewHBox(addBtn),
		nil, nil, nil,
		p.scroll,
	)

	reg.SetOnAdd(func(pl plugin.Plugin) {
		fyne.Do(func() {
			p.rebuild()
		})
		p.save()
	})
	reg.SetOnRemove(func(pl plugin.Plugin) {
		p.save()
	})

	p.ExtendBaseWidget(p)

	// Restore persisted charts
	p.loadSaved()

	return p
}

// save persists the current chart configs to disk.
func (p *PluginsPanel) save() {
	var cfgs []plugin.ChartConfig
	for _, pl := range p.registry.Plugins() {
		if c, ok := pl.(*chart.Plugin); ok {
			cfgs = append(cfgs, plugin.ChartConfig{
				Name:    c.Name(),
				Pattern: c.Pattern(),
			})
		}
	}
	_ = plugin.SaveCharts(cfgs) // best-effort
}

// loadSaved restores charts persisted from previous runs.
func (p *PluginsPanel) loadSaved() {
	cfgs, err := plugin.LoadCharts()
	if err != nil || len(cfgs) == 0 {
		return
	}
	for _, cfg := range cfgs {
		cfg := cfg // capture
		var pl *chart.Plugin
		pl, err := chart.New(cfg.Name, cfg.Pattern, func() {
			p.registry.Remove(pl)
			fyne.Do(func() {
				p.rebuild()
			})
		})
		if err != nil {
			continue // skip broken patterns silently
		}
		p.registry.Add(pl)
	}
}

// SetWindow wires the parent window for dialogs.
func (p *PluginsPanel) SetWindow(w fyne.Window) {
	p.window = w
}

// Content returns the panel's UI tree.
func (p *PluginsPanel) Content() fyne.CanvasObject {
	return p.content
}

// rebuild recreates the widget list from the registry.
func (p *PluginsPanel) rebuild() {
	pls := p.registry.Plugins()
	if len(pls) == 0 {
		p.vbox.Objects = []fyne.CanvasObject{p.empty}
	} else {
		objs := make([]fyne.CanvasObject, len(pls))
		for i, pl := range pls {
			objs[i] = pl.Widget()
		}
		p.vbox.Objects = objs
	}
	p.vbox.Refresh()
}

// showAddChartDialog opens the dialog to configure a new chart plugin.
func (p *PluginsPanel) showAddChartDialog() {
	if p.window == nil {
		return
	}

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Chart name (e.g. sr_perf_01)")

	patternEntry := widget.NewEntry()
	patternEntry.SetPlaceHolder("e.g. Session Replay telemetry: sr_perf_01={%d}ms")

	hint := widget.NewLabel("Use {%d} for integers, {%f} for decimals.\nEverything else must match literally.")
	hint.Wrapping = fyne.TextWrapWord

	form := container.NewVBox(
		widget.NewLabel("Name:"),
		nameEntry,
		widget.NewLabel("Pattern:"),
		patternEntry,
		hint,
	)

	d := dialog.NewCustomConfirm("Add chart", "Add", "Cancel", form, func(ok bool) {
		if !ok {
			return
		}
		name := nameEntry.Text
		if name == "" {
			name = "chart"
		}
		pattern := patternEntry.Text
		if pattern == "" {
			return
		}

		var pl *chart.Plugin
		var err error
		pl, err = chart.New(name, pattern, func() {
			// onClose: remove from registry and rebuild on UI thread
			p.registry.Remove(pl)
			fyne.Do(func() {
				p.rebuild()
			})
		})
		if err != nil {
			dialog.ShowError(err, p.window)
			return
		}
		p.registry.Add(pl)
	}, p.window)
	d.Resize(fyne.NewSize(500, 300))
	d.Show()
}

// CreateRenderer implements fyne.Widget.
func (p *PluginsPanel) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.content)
}
