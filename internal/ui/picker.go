package ui

import (
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// TargetKind tells what a picker entry opens.
type TargetKind int

const (
	// TargetDevice is a device connected to adb: USB, Wi-Fi, or an emulator
	// that's already running. Logs and plugins work; there's no screen.
	TargetDevice TargetKind = iota
	// TargetAVD is an AVD to launch, with its screen embedded.
	TargetAVD
)

// Target is an entry of the picker.
type Target struct {
	Kind   TargetKind
	ID     string // adb serial or AVD name
	Title  string
	Detail string
	// Ready is false for devices that can't be opened yet (unauthorized,
	// offline...); Detail says why.
	Ready bool
}

// pickerRow is a section header, a note (e.g. "no devices") or a target.
type pickerRow struct {
	header string
	note   string
	target Target
}

func (r pickerRow) isTarget() bool { return r.header == "" && r.note == "" }

// PickerPanel lists the connected devices and the AVDs that can be
// launched.
type PickerPanel struct {
	widget.BaseWidget

	devices, avds         []Target
	devicesNote, avdsNote string
	rows                  []pickerRow

	onSelect  func(Target)
	onWiFi    func()
	onRefresh func()

	status  *widget.Label
	list    *widget.List
	content fyne.CanvasObject

	lineH float32
}

// NewPickerPanel creates the picker.
func NewPickerPanel(onSelect func(Target)) *PickerPanel {
	p := &PickerPanel{onSelect: onSelect}

	p.status = widget.NewLabel("")
	p.status.Alignment = fyne.TextAlignCenter
	p.status.Wrapping = fyne.TextWrapWord

	p.list = widget.NewList(
		func() int { return len(p.rows) },
		func() fyne.CanvasObject {
			title := widget.NewLabel("template")
			title.Truncation = fyne.TextTruncateEllipsis
			detail := widget.NewLabel("template")
			detail.Truncation = fyne.TextTruncateEllipsis
			detail.SizeName = theme.SizeNameCaptionText
			return container.NewVBox(title, detail)
		},
		p.updateRow,
	)
	p.list.OnSelected = func(id widget.ListItemID) {
		p.list.Unselect(id) // allow selecting the same entry again later
		if id < 0 || id >= len(p.rows) || !p.rows[id].isTarget() {
			return
		}
		t := p.rows[id].target
		if !t.Ready {
			p.SetStatus(t.Title + ": " + t.Detail)
			return
		}
		if p.onSelect != nil {
			p.onSelect(t)
		}
	}
	p.lineH = widget.NewLabel("M").MinSize().Height

	title := widget.NewLabel("Android Companion")
	title.Alignment = fyne.TextAlignCenter
	title.TextStyle = fyne.TextStyle{Bold: true}
	subtitle := widget.NewLabel("Select a device, or an emulator to launch")
	subtitle.Alignment = fyne.TextAlignCenter

	wifi := widget.NewButton("Pair device over Wi-Fi…", func() {
		if p.onWiFi != nil {
			p.onWiFi()
		}
	})
	refresh := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
		if p.onRefresh != nil {
			p.onRefresh()
		}
	})
	footer := container.NewVBox(
		p.status,
		container.NewHBox(layout.NewSpacer(), wifi, refresh, layout.NewSpacer()),
	)
	p.content = container.NewBorder(container.NewVBox(title, subtitle), footer, nil, nil, p.list)

	p.rebuild()
	p.ExtendBaseWidget(p)
	return p
}

func (p *PickerPanel) updateRow(id widget.ListItemID, obj fyne.CanvasObject) {
	if id < 0 || id >= len(p.rows) {
		return
	}
	row := p.rows[id]
	box := obj.(*fyne.Container)
	title := box.Objects[0].(*widget.Label)
	detail := box.Objects[1].(*widget.Label)

	switch {
	case row.header != "":
		title.TextStyle = fyne.TextStyle{Bold: true}
		title.Importance = widget.HighImportance
		title.SetText(row.header)
		detail.Hide()
	case row.note != "":
		title.TextStyle = fyne.TextStyle{Italic: true}
		title.Importance = widget.LowImportance
		title.SetText(row.note)
		detail.Hide()
	default:
		t := row.target
		title.TextStyle = fyne.TextStyle{}
		title.Importance = widget.MediumImportance
		detail.Importance = widget.MediumImportance
		if !t.Ready {
			title.Importance = widget.LowImportance
			detail.Importance = widget.WarningImportance
		}
		title.SetText(t.Title)
		detail.SetText(t.Detail)
		detail.Show()
	}
}

// SetDevices sets the connected devices. note is shown when there are
// none (or explains an error).
func (p *PickerPanel) SetDevices(devices []Target, note string) {
	if slices.Equal(devices, p.devices) && note == p.devicesNote {
		return
	}
	p.devices, p.devicesNote = devices, note
	p.rebuild()
}

// SetAVDs sets the AVDs that can be launched. note is shown when there
// are none.
func (p *PickerPanel) SetAVDs(avds []Target, note string) {
	if slices.Equal(avds, p.avds) && note == p.avdsNote {
		return
	}
	p.avds, p.avdsNote = avds, note
	p.rebuild()
}

func (p *PickerPanel) rebuild() {
	p.rows = p.rows[:0]
	section := func(header string, targets []Target, note string) {
		p.rows = append(p.rows, pickerRow{header: header})
		for _, t := range targets {
			p.rows = append(p.rows, pickerRow{target: t})
		}
		if note != "" {
			p.rows = append(p.rows, pickerRow{note: note})
		}
	}
	section("Connected devices", p.devices, p.devicesNote)
	section("Emulators", p.avds, p.avdsNote)

	// Headers and notes are a single line; targets have a detail line.
	twoLines := p.list.CreateItem().MinSize().Height
	for id, row := range p.rows {
		h := twoLines
		if !row.isTarget() {
			h = p.lineH
		}
		p.list.SetItemHeight(id, h)
	}
	p.list.Refresh()
}

// SetOnSelect sets the callback when a target is selected.
func (p *PickerPanel) SetOnSelect(f func(Target)) {
	p.onSelect = f
}

// SetOnWiFi sets the action of the "Pair device over Wi-Fi" button.
func (p *PickerPanel) SetOnWiFi(f func()) {
	p.onWiFi = f
}

// SetOnRefresh sets the action of the refresh button.
func (p *PickerPanel) SetOnRefresh(f func()) {
	p.onRefresh = f
}

// SetStatus shows a status message.
func (p *PickerPanel) SetStatus(s string) {
	p.status.SetText(s)
}

// Content returns the picker's UI tree.
func (p *PickerPanel) Content() fyne.CanvasObject {
	return p.content
}

// CreateRenderer is required by fyne.Widget.
func (p *PickerPanel) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.content)
}
