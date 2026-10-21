package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// PickerPanel shows the list of available AVDs to launch.
type PickerPanel struct {
	widget.BaseWidget

	avds     []string
	onSelect func(name string)
	status   *widget.Label
	list     *widget.List
}

// NewPickerPanel creates the picker.
func NewPickerPanel(onSelect func(name string)) *PickerPanel {
	p := &PickerPanel{onSelect: onSelect}

	p.status = widget.NewLabel("")
	p.status.Alignment = fyne.TextAlignCenter

	p.list = widget.NewList(
		func() int { return len(p.avds) },
		func() fyne.CanvasObject {
			return widget.NewLabel("template")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(p.avds) {
				return
			}
			obj.(*widget.Label).SetText(p.avds[id])
		},
	)
	p.list.OnSelected = func(id widget.ListItemID) {
		if id >= 0 && id < len(p.avds) && p.onSelect != nil {
			p.onSelect(p.avds[id])
		}
	}

	p.ExtendBaseWidget(p)
	return p
}

// SetAVDs fills the list.
func (p *PickerPanel) SetAVDs(avds []string) {
	p.avds = avds
	p.list.Refresh()
}

// SetOnSelect sets the callback when a device is selected.
func (p *PickerPanel) SetOnSelect(f func(name string)) {
	p.onSelect = f
}

// SetStatus shows a status message.
func (p *PickerPanel) SetStatus(s string) {
	p.status.SetText(s)
}

// Content returns the picker's UI tree.
func (p *PickerPanel) Content() fyne.CanvasObject {
	title := widget.NewLabel("Android Companion")
	title.Alignment = fyne.TextAlignCenter
	title.TextStyle = fyne.TextStyle{Bold: true}

	subtitle := widget.NewLabel("Select a device to launch")
	subtitle.Alignment = fyne.TextAlignCenter

	header := container.NewVBox(title, subtitle)
	return container.NewBorder(header, p.status, nil, nil, p.list)
}

// CreateRenderer is required by fyne.Widget.
func (p *PickerPanel) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.Content())
}
