package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

	"github.com/rmarkiewicz/android-companion/internal/logcat"
)

// MainWindow holds the top-level UI with the resizable HSplit.
type MainWindow struct {
	window fyne.Window

	split    *container.Split
	picker   *PickerPanel
	emulator *EmulatorWidget
	navbar   *NavBar
	logcat   *LogcatPanel

	leftContent fyne.CanvasObject
}

// NewMainWindow builds the window content and returns the controller.
func NewMainWindow(win fyne.Window, store *logcat.Store) *MainWindow {
	mw := &MainWindow{window: win}

	mw.picker = NewPickerPanel(nil) // onSelect wired later by app
	mw.emulator = NewEmulatorWidget()
	mw.navbar = NewNavBar(nil) // onKey wired later by app
	mw.logcat = NewLogcatPanel(store)

	// Left side starts as the picker.
	mw.leftContent = mw.picker.Content()

	// HSplit: left = picker/emulator, right = logcat. Draggable divider.
	mw.split = container.NewHSplit(mw.leftContent, mw.logcat.Content())
	mw.split.SetOffset(0.55) // left takes 55% initially

	win.SetContent(mw.split)
	win.Resize(fyne.NewSize(1280, 800))

	// Wire window for the package filter dialog
	mw.logcat.SetWindow(win)

	return mw
}

// Picker returns the picker panel.
func (mw *MainWindow) Picker() *PickerPanel { return mw.picker }

// Emulator returns the emulator widget.
func (mw *MainWindow) Emulator() *EmulatorWidget { return mw.emulator }

// NavBar returns the navigation bar.
func (mw *MainWindow) NavBar() *NavBar { return mw.navbar }

// Logcat returns the logcat panel.
func (mw *MainWindow) Logcat() *LogcatPanel { return mw.logcat }

// ShowEmulator switches the left panel from picker to the emulator view,
// with the navigation bar (back/home/recents) below the screen.
func (mw *MainWindow) ShowEmulator() {
	mw.split.Leading = container.NewBorder(nil, mw.navbar.Content(), nil, nil, mw.emulator)
	mw.split.Refresh()
}

// ShowPicker switches the left panel back to the picker.
func (mw *MainWindow) ShowPicker() {
	mw.split.Leading = mw.picker.Content()
	mw.split.Refresh()
}

// SetTitle updates the window title.
func (mw *MainWindow) SetTitle(t string) {
	mw.window.SetTitle(t)
}
