package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/rmarkiewicz/android-companion/internal/plugin"
	"github.com/rmarkiewicz/android-companion/internal/plugin/chart"
	"github.com/rmarkiewicz/android-companion/internal/plugin/status"
)

// PluginsPanel is the right-side column holding active plugin widgets,
// with "Add chart" / "Add status" buttons and per-widget close buttons.
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

	p.empty = widget.NewLabel("No widgets yet.\nAdd a chart or a status.")
	p.empty.Alignment = fyne.TextAlignCenter
	p.empty.Wrapping = fyne.TextWrapWord

	p.vbox = container.NewVBox(p.empty)
	p.scroll = container.NewVScroll(p.vbox)

	addChartBtn := widget.NewButtonWithIcon("Add chart", theme.ContentAddIcon(), p.showAddChartDialog)
	addStatusBtn := widget.NewButtonWithIcon("Add status", theme.ContentAddIcon(), p.showAddStatusDialog)

	p.content = container.NewBorder(
		container.NewHBox(addChartBtn, addStatusBtn),
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

	// Restore persisted plugins
	p.loadSaved()

	return p
}

// save persists all plugin configs to disk.
func (p *PluginsPanel) save() {
	var charts []plugin.ChartConfig
	var statuses []plugin.StatusConfig
	for _, pl := range p.registry.Plugins() {
		switch c := pl.(type) {
		case *chart.Plugin:
			charts = append(charts, plugin.ChartConfig{
				Name:    c.Name(),
				Pattern: c.Pattern(),
			})
		case *status.Plugin:
			var rules []plugin.StatusRule
			for _, r := range c.Rules() {
				rules = append(rules, plugin.StatusRule{
					Pattern: r.Pattern,
					Label:   r.Label,
					Color:   r.Color,
				})
			}
			statuses = append(statuses, plugin.StatusConfig{
				Name:  c.Name(),
				Rules: rules,
			})
		}
	}
	_ = plugin.SaveCharts(charts)     // best-effort
	_ = plugin.SaveStatuses(statuses) // best-effort
}

// loadSaved restores plugins persisted from previous runs.
func (p *PluginsPanel) loadSaved() {
	if cfgs, err := plugin.LoadCharts(); err == nil {
		for _, cfg := range cfgs {
			cfg := cfg
			var pl *chart.Plugin
			pl, err := chart.New(cfg.Name, cfg.Pattern, func() {
				p.registry.Remove(pl)
				fyne.Do(func() { p.rebuild() })
			})
			if err != nil {
				continue
			}
			p.registry.Add(pl)
		}
	}
	if cfgs, err := plugin.LoadStatuses(); err == nil {
		for _, cfg := range cfgs {
			cfg := cfg
			var rules []status.Rule
			for _, r := range cfg.Rules {
				rules = append(rules, status.Rule{
					Pattern: r.Pattern,
					Label:   r.Label,
					Color:   r.Color,
				})
			}
			var pl *status.Plugin
			pl, err := status.New(cfg.Name, rules, func() {
				p.registry.Remove(pl)
				fyne.Do(func() { p.rebuild() })
			})
			if err != nil {
				continue
			}
			p.registry.Add(pl)
		}
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
			p.registry.Remove(pl)
			fyne.Do(func() { p.rebuild() })
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

// ruleRow is one editable rule in the status dialog.
type ruleRow struct {
	pattern *widget.Entry
	label   *widget.Entry
	color   *widget.Entry
	swath   *canvas.Rectangle
	box     *fyne.Container
}

func newRuleRow(onChange func()) *ruleRow {
	r := &ruleRow{}
	r.pattern = widget.NewEntry()
	r.pattern.SetPlaceHolder("log substring")
	r.label = widget.NewEntry()
	r.label.SetPlaceHolder("label")
	r.color = widget.NewEntry()
	r.color.SetPlaceHolder("#4CAF50")
	r.swath = canvas.NewRectangle(theme.Color(theme.ColorNamePrimary))
	r.swath.SetMinSize(fyne.NewSize(24, 24))
	r.swath.CornerRadius = 4

	r.color.OnChanged = func(s string) {
		if c, err := status.ParseHexColor(s); err == nil {
			r.swath.FillColor = c
			r.swath.Refresh()
		}
		if onChange != nil {
			onChange()
		}
	}

	r.box = container.NewBorder(nil, nil, nil, container.NewHBox(r.color, r.swath),
		container.NewGridWithColumns(2, r.pattern, r.label),
	)
	return r
}

func (r *ruleRow) toRule() status.Rule {
	return status.Rule{
		Pattern: r.pattern.Text,
		Label:   r.label.Text,
		Color:   r.color.Text,
	}
}

// showAddStatusDialog opens the dialog to configure a status plugin
// with N rules (pattern + label + color each).
func (p *PluginsPanel) showAddStatusDialog() {
	if p.window == nil {
		return
	}

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Status name (e.g. Session Replay)")

	rulesBox := container.NewVBox()
	var rows []*ruleRow
	var dlg dialog.Dialog

	addRow := func() {
		row := newRuleRow(nil)
		rows = append(rows, row)
		rulesBox.Add(row.box)
		rulesBox.Refresh()
		// Grow the dialog so the new row is visible
		if dlg != nil {
			sz := dlg.MinSize()
			dlg.Resize(fyne.NewSize(sz.Width, sz.Height+44))
		}
	}
	addRow() // start with one row

	addRuleBtn := widget.NewButtonWithIcon("Add rule", theme.ContentAddIcon(), addRow)

	hint := widget.NewLabel("Each rule: when a log line matches the pattern, the badge\nswitches to that label + color.\nPatterns support {%d} {%f} {%s} captures; use $0, $1... in the label\nto show captured values.")
	hint.Wrapping = fyne.TextWrapWord

	// Fixed-height scroll area for rules so rows are always visible.
	rulesScroll := container.NewVScroll(rulesBox)
	rulesScroll.SetMinSize(fyne.NewSize(520, 160))

	form := container.NewVBox(
		widget.NewLabel("Name:"),
		nameEntry,
		widget.NewLabel("Rules:"),
		rulesScroll,
		addRuleBtn,
		hint,
	)

	d := dialog.NewCustomConfirm("Add status", "Add", "Cancel", form, func(ok bool) {
		dlg = nil
		if !ok {
			return
		}
		name := nameEntry.Text
		if name == "" {
			name = "status"
		}
		var rules []status.Rule
		for _, row := range rows {
			r := row.toRule()
			if r.Pattern != "" && r.Label != "" {
				rules = append(rules, r)
			}
		}
		if len(rules) == 0 {
			dialog.ShowError(fmt.Errorf("add at least one rule with pattern and label"), p.window)
			return
		}

		var pl *status.Plugin
		var err error
		pl, err = status.New(name, rules, func() {
			p.registry.Remove(pl)
			fyne.Do(func() { p.rebuild() })
		})
		if err != nil {
			dialog.ShowError(err, p.window)
			return
		}
		p.registry.Add(pl)
	}, p.window)
	dlg = d
	d.Resize(fyne.NewSize(580, 400))
	d.Show()
}

// CreateRenderer implements fyne.Widget.
func (p *PluginsPanel) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.content)
}
