package ui

import (
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// PackageFilter is a compact package selector: a button showing the current
// selection, which opens a searchable dialog when tapped.
type PackageFilter struct {
	widget.BaseWidget

	mu       sync.RWMutex
	all      []string
	selected string

	button   *widget.Button
	onSelect func(pkg string)
	window   fyne.Window // parent window for dialog
}

// NewPackageFilter creates the widget.
func NewPackageFilter(onSelect func(pkg string)) *PackageFilter {
	p := &PackageFilter{onSelect: onSelect}
	p.button = widget.NewButton("Select package...", p.showDialog)
	p.button.Alignment = widget.ButtonAlignLeading
	p.ExtendBaseWidget(p)
	return p
}

// SetWindow sets the parent window for dialogs.
func (p *PackageFilter) SetWindow(w fyne.Window) {
	p.window = w
}

// SetPackages updates the full list of packages.
func (p *PackageFilter) SetPackages(pkgs []string) {
	p.mu.Lock()
	p.all = append([]string{}, pkgs...)
	p.mu.Unlock()
}

// Selected returns the currently selected package.
func (p *PackageFilter) Selected() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.selected
}

// Clear resets the selection.
func (p *PackageFilter) Clear() {
	p.SetSelected("")
}

// SetSelected shows pkg as the selection, without calling onSelect.
func (p *PackageFilter) SetSelected(pkg string) {
	p.mu.Lock()
	p.selected = pkg
	p.mu.Unlock()
	if pkg == "" {
		p.button.SetText("Select package...")
	} else {
		p.button.SetText(pkg)
	}
}

// Content returns the widget's UI tree (just the button).
func (p *PackageFilter) Content() fyne.CanvasObject {
	return p.button
}

// CreateRenderer is required by fyne.Widget.
func (p *PackageFilter) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.button)
}

// showDialog opens a searchable list dialog.
func (p *PackageFilter) showDialog() {
	if p.window == nil {
		return
	}
	p.mu.RLock()
	all := p.all
	p.mu.RUnlock()

	// Search entry
	search := widget.NewEntry()
	search.SetPlaceHolder("Search packages...")

	// Filtered list
	filtered := append([]string{}, all...)
	list := widget.NewList(
		func() int { return len(filtered) },
		func() fyne.CanvasObject {
			return widget.NewLabel("template")
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= 0 && id < len(filtered) {
				obj.(*widget.Label).SetText(filtered[id])
			}
		},
	)

	// Update filtered list when search changes
	search.OnChanged = func(s string) {
		if s == "" {
			filtered = append([]string{}, all...)
		} else {
			lower := strings.ToLower(s)
			filtered = filtered[:0]
			for _, pkg := range all {
				if strings.Contains(strings.ToLower(pkg), lower) {
					filtered = append(filtered, pkg)
				}
			}
		}
		list.Refresh()
	}

	// Handle selection
	var dlg dialog.Dialog
	list.OnSelected = func(id widget.ListItemID) {
		if id >= 0 && id < len(filtered) {
			pkg := filtered[id]
			p.mu.Lock()
			p.selected = pkg
			p.mu.Unlock()
			p.button.SetText(pkg)
			if p.onSelect != nil {
				p.onSelect(pkg)
			}
			if dlg != nil {
				dlg.Hide()
			}
		}
	}

	// "All packages" clear option
	clearBtn := widget.NewButton("Clear (show all)", func() {
		p.Clear()
		if p.onSelect != nil {
			p.onSelect("")
		}
		if dlg != nil {
			dlg.Hide()
		}
	})

	content := container.NewBorder(search, clearBtn, nil, nil, list)
	dlg = dialog.NewCustom("Select package", "Cancel", content, p.window)
	dlg.Resize(fyne.NewSize(400, 500))
	dlg.Show()
}
