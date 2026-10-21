package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// NavKey is an Android navigation button.
type NavKey int

const (
	NavBack NavKey = iota + 1
	NavHome
	NavRecents
)

// NavBar is the row of Android navigation buttons shown under the
// screen: back, home, recents.
type NavBar struct {
	widget.BaseWidget

	onKey func(NavKey)

	row *fyne.Container
}

// NewNavBar creates the bar.
func NewNavBar(onKey func(NavKey)) *NavBar {
	n := &NavBar{onKey: onKey}

	back := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		n.press(NavBack)
	})
	home := widget.NewButtonWithIcon("", theme.HomeIcon(), func() {
		n.press(NavHome)
	})
	recents := widget.NewButtonWithIcon("", theme.MenuIcon(), func() {
		n.press(NavRecents)
	})

	n.row = container.NewHBox(
		layout.NewSpacer(),
		back, home, recents,
		layout.NewSpacer(),
	)

	n.ExtendBaseWidget(n)
	return n
}

func (n *NavBar) press(k NavKey) {
	if n.onKey != nil {
		n.onKey(k)
	}
}

// Content returns the bar's UI tree.
func (n *NavBar) Content() fyne.CanvasObject {
	return n.row
}

// SetOnKey sets the callback for the buttons.
func (n *NavBar) SetOnKey(f func(NavKey)) {
	n.onKey = f
}

// CreateRenderer implements fyne.Widget.
func (n *NavBar) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(n.row)
}
