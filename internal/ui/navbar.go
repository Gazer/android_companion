package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// NavBar is the row of Android navigation buttons shown under the
// emulator screen: back, home, recents.
type NavBar struct {
	widget.BaseWidget

	onKey func(keyCode int32)

	row *fyne.Container
}

// Evdev key codes for Android navigation (same as the emulator's gRPC
// sendKey expects with Evdev code type).
const (
	evdevKeyBack      = 158 // KEY_BACK
	evdevKeyHome      = 172 // KEY_HOMEPAGE
	evdevKeyAppSwitch = 580 // KEY_APPSELECT (recents)
)

// NewNavBar creates the bar. onKey receives evdev key codes.
func NewNavBar(onKey func(keyCode int32)) *NavBar {
	n := &NavBar{onKey: onKey}

	back := widget.NewButtonWithIcon("", theme.NavigateBackIcon(), func() {
		n.press(evdevKeyBack)
	})
	home := widget.NewButtonWithIcon("", theme.HomeIcon(), func() {
		n.press(evdevKeyHome)
	})
	recents := widget.NewButtonWithIcon("", theme.MenuIcon(), func() {
		n.press(evdevKeyAppSwitch)
	})

	n.row = container.NewHBox(
		layout.NewSpacer(),
		back, home, recents,
		layout.NewSpacer(),
	)

	n.ExtendBaseWidget(n)
	return n
}

func (n *NavBar) press(code int32) {
	if n.onKey != nil {
		n.onKey(code)
	}
}

// Content returns the bar's UI tree.
func (n *NavBar) Content() fyne.CanvasObject {
	return n.row
}

// SetOnKey sets the callback receiving evdev key codes.
func (n *NavBar) SetOnKey(f func(keyCode int32)) {
	n.onKey = f
}

// CreateRenderer implements fyne.Widget.
func (n *NavBar) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(n.row)
}
