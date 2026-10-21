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
	"github.com/rmarkiewicz/android-companion/internal/plugin/counter"
	"github.com/rmarkiewicz/android-companion/internal/plugin/status"
)

// PluginsPanel is the right-side column holding active plugin widgets,
// with an "Add" menu (chart, status, counter) and per-widget edit and
// close buttons.
type PluginsPanel struct {
	widget.BaseWidget

	registry *plugin.Registry
	window   fyne.Window

	vbox    *fyne.Container
	scroll  *container.Scroll
	empty   *widget.Label
	content *fyne.Container
	addMenu *fyne.Menu

	// restoring is true while loadSaved runs: Add() fires during restore,
	// and we must not persist until everything is loaded (otherwise each
	// restored chart would rewrite statuses.json as empty before the
	// statuses themselves are loaded).
	restoring bool
}

// NewPluginsPanel creates the panel wired to the plugin registry.
func NewPluginsPanel(reg *plugin.Registry) *PluginsPanel {
	p := &PluginsPanel{registry: reg}

	p.empty = widget.NewLabel("No widgets yet.\nAdd a chart, a status or a counter.")
	p.empty.Alignment = fyne.TextAlignCenter
	p.empty.Wrapping = fyne.TextWrapWord

	p.vbox = container.NewVBox(p.empty)
	p.scroll = container.NewVScroll(p.vbox)

	p.addMenu = fyne.NewMenu("",
		fyne.NewMenuItem("Chart", func() { p.showChartDialog(nil) }),
		fyne.NewMenuItem("Status", func() { p.showStatusDialog(nil) }),
		fyne.NewMenuItem("Counter", func() { p.showCounterDialog(nil) }),
	)
	var addBtn *widget.Button
	addBtn = widget.NewButtonWithIcon("Add", theme.ContentAddIcon(), func() {
		if c := fyne.CurrentApp().Driver().CanvasForObject(addBtn); c != nil {
			widget.ShowPopUpMenuAtRelativePosition(p.addMenu, c, fyne.NewPos(0, addBtn.Size().Height), addBtn)
		}
	})

	p.content = container.NewBorder(
		container.NewHBox(addBtn),
		nil, nil, nil,
		p.scroll,
	)

	reg.SetOnAdd(func(pl plugin.Plugin) {
		fyne.Do(func() {
			p.rebuild()
		})
		if !p.restoring {
			p.save()
		}
	})
	reg.SetOnRemove(func(pl plugin.Plugin) {
		if !p.restoring {
			p.save()
		}
	})

	p.ExtendBaseWidget(p)

	// Restore persisted plugins
	p.restoring = true
	p.loadSaved()
	p.restoring = false

	return p
}

// save persists all plugin configs to disk.
func (p *PluginsPanel) save() {
	var charts []plugin.ChartConfig
	var statuses []plugin.StatusConfig
	var counters []plugin.CounterConfig
	for _, pl := range p.registry.Plugins() {
		switch c := pl.(type) {
		case *counter.Plugin:
			counters = append(counters, plugin.CounterConfig{
				Name:    c.Name(),
				Pattern: c.Pattern(),
			})
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
	if err := plugin.SaveCharts(charts); err != nil {
		fmt.Printf("[plugins] save charts failed: %v\n", err)
	}
	if err := plugin.SaveStatuses(statuses); err != nil {
		fmt.Printf("[plugins] save statuses failed: %v\n", err)
	}
	if err := plugin.SaveCounters(counters); err != nil {
		fmt.Printf("[plugins] save counters failed: %v\n", err)
	}
}

// loadSaved restores plugins persisted from previous runs.
func (p *PluginsPanel) loadSaved() {
	if cfgs, err := plugin.LoadCharts(); err == nil {
		for _, cfg := range cfgs {
			cfg := cfg
			pl, err := chart.New(cfg.Name, cfg.Pattern, nil)
			if err != nil {
				continue
			}
			p.addChart(pl)
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
			pl, err := status.New(cfg.Name, rules, nil)
			if err != nil {
				continue
			}
			p.addStatus(pl)
		}
	}
	if cfgs, err := plugin.LoadCounters(); err == nil {
		for _, cfg := range cfgs {
			pl, err := counter.New(cfg.Name, cfg.Pattern, nil)
			if err != nil {
				continue
			}
			p.addCounter(pl)
		}
	}
}

// addCounter wires the plugin's close/edit callbacks and registers it.
func (p *PluginsPanel) addCounter(pl *counter.Plugin) {
	pl.SetOnClose(func() {
		p.registry.Remove(pl)
		fyne.Do(func() { p.rebuild() })
	})
	pl.SetOnEdit(func() {
		p.showCounterDialog(pl)
	})
	p.registry.Add(pl)
}

// addChart wires the plugin's close/edit callbacks and registers it.
func (p *PluginsPanel) addChart(pl *chart.Plugin) {
	pluginPtr := pl
	pl.SetOnClose(func() {
		p.registry.Remove(pluginPtr)
		fyne.Do(func() { p.rebuild() })
	})
	pl.SetOnEdit(func() {
		p.showChartDialog(pluginPtr)
	})
	p.registry.Add(pl)
}

// addStatus wires the plugin's close/edit callbacks and registers it.
func (p *PluginsPanel) addStatus(pl *status.Plugin) {
	pluginPtr := pl
	pl.SetOnClose(func() {
		p.registry.Remove(pluginPtr)
		fyne.Do(func() { p.rebuild() })
	})
	pl.SetOnEdit(func() {
		p.showStatusDialog(pluginPtr)
	})
	p.registry.Add(pl)
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

// showChartDialog opens the chart config dialog. If existing is non-nil,
// the dialog is pre-filled and confirming replaces that plugin (edit mode).
func (p *PluginsPanel) showChartDialog(existing *chart.Plugin) {
	if p.window == nil {
		return
	}

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Chart name (e.g. sr_perf_01)")
	patternEntry := widget.NewEntry()
	patternEntry.SetPlaceHolder("e.g. Session Replay telemetry: sr_perf_01={%d}ms")

	title, confirm := "Add chart", "Add"
	if existing != nil {
		nameEntry.SetText(existing.Name())
		patternEntry.SetText(existing.Pattern())
		title, confirm = "Edit chart", "Save"
	}

	hint := widget.NewLabel("Use {%d} for integers, {%f} for decimals.\nEverything else must match literally.")
	hint.Wrapping = fyne.TextWrapWord

	form := container.NewVBox(
		widget.NewLabel("Name:"),
		nameEntry,
		widget.NewLabel("Pattern:"),
		patternEntry,
		hint,
	)

	d := dialog.NewCustomConfirm(title, confirm, "Cancel", form, func(ok bool) {
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

		pl, err := chart.New(name, pattern, nil)
		if err != nil {
			dialog.ShowError(err, p.window)
			return
		}
		if existing != nil {
			p.registry.Remove(existing)
		}
		p.addChart(pl)
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

func newRuleRow(initial *status.Rule) *ruleRow {
	r := &ruleRow{}
	r.pattern = widget.NewEntry()
	r.pattern.SetPlaceHolder("log pattern, {%d} {%f} {%s} ok")
	r.label = widget.NewEntry()
	r.label.SetPlaceHolder("label, $0 $1 ok")
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
	}

	if initial != nil {
		r.pattern.SetText(initial.Pattern)
		r.label.SetText(initial.Label)
		r.color.SetText(initial.Color)
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

// showStatusDialog opens the status config dialog. If existing is
// non-nil, the dialog is pre-filled and confirming replaces it (edit).
func (p *PluginsPanel) showStatusDialog(existing *status.Plugin) {
	if p.window == nil {
		return
	}

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Status name (e.g. Session Replay)")

	title, confirm := "Add status", "Add"
	if existing != nil {
		nameEntry.SetText(existing.Name())
		title, confirm = "Edit status", "Save"
	}

	rulesBox := container.NewVBox()
	var rows []*ruleRow
	var dlg dialog.Dialog

	addRow := func(initial *status.Rule) {
		row := newRuleRow(initial)
		rows = append(rows, row)
		rulesBox.Add(row.box)
		rulesBox.Refresh()
		if dlg != nil {
			sz := dlg.MinSize()
			dlg.Resize(fyne.NewSize(sz.Width, sz.Height+44))
		}
	}

	if existing != nil {
		for _, r := range existing.Rules() {
			r := r
			addRow(&r)
		}
	} else {
		addRow(nil) // start with one empty row
	}

	addRuleBtn := widget.NewButtonWithIcon("Add rule", theme.ContentAddIcon(), func() {
		addRow(nil)
	})

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

	d := dialog.NewCustomConfirm(title, confirm, "Cancel", form, func(ok bool) {
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

		pl, err := status.New(name, rules, nil)
		if err != nil {
			dialog.ShowError(err, p.window)
			return
		}
		if existing != nil {
			p.registry.Remove(existing)
		}
		p.addStatus(pl)
	}, p.window)
	dlg = d
	d.Resize(fyne.NewSize(580, 400))
	d.Show()
}

// showCounterDialog opens the counter config dialog. If existing is
// non-nil, the dialog is pre-filled and confirming replaces it (edit); the
// count is kept if the pattern doesn't change.
func (p *PluginsPanel) showCounterDialog(existing *counter.Plugin) {
	if p.window == nil {
		return
	}

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Counter name (e.g. Crashes)")
	patternEntry := widget.NewEntry()
	patternEntry.SetPlaceHolder("e.g. FATAL EXCEPTION")

	title, confirm := "Add counter", "Add"
	if existing != nil {
		nameEntry.SetText(existing.Name())
		patternEntry.SetText(existing.Pattern())
		title, confirm = "Edit counter", "Save"
	}

	hint := widget.NewLabel("Counts the log lines that contain the pattern (case-sensitive).\n" +
		"{%d} {%f} {%s} match any number / word.")

	form := container.NewVBox(
		widget.NewLabel("Name:"),
		nameEntry,
		widget.NewLabel("Pattern:"),
		patternEntry,
		hint,
	)

	d := dialog.NewCustomConfirm(title, confirm, "Cancel", form, func(ok bool) {
		if !ok || patternEntry.Text == "" {
			return
		}
		name := nameEntry.Text
		if name == "" {
			name = "counter"
		}
		pl, err := counter.New(name, patternEntry.Text, nil)
		if err != nil {
			dialog.ShowError(err, p.window)
			return
		}
		if existing != nil {
			if existing.Pattern() == pl.Pattern() {
				pl.SetCount(existing.Count())
			}
			p.registry.Remove(existing)
		}
		p.addCounter(pl)
	}, p.window)
	d.Resize(fyne.NewSize(500, 300))
	d.Show()
}

// CreateRenderer implements fyne.Widget.
func (p *PluginsPanel) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.content)
}
