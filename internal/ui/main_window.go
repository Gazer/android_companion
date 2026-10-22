package ui

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/rmarkiewicz/android-companion/internal/logcat"
	"github.com/rmarkiewicz/android-companion/internal/plugin"

	"github.com/fyne-io/terminal"
)

// MainWindow holds the top-level UI: emulator/device | logcat | plugins.
type MainWindow struct {
	window fyne.Window

	split   *container.Split
	picker  *PickerPanel
	screen  *ScreenWidget
	navbar  *NavBar
	logcat  *LogcatPanel
	plugins *PluginsPanel

	pluginsVisible bool
	toggleBtn      *widget.Button
	leftContent    fyne.CanvasObject
	rightSplit     *container.Split
	rightContent   fyne.CanvasObject
	onDisconnect   func()
}

// NewMainWindow builds the window content and returns the controller.
func NewMainWindow(win fyne.Window, store *logcat.Store, registry *plugin.Registry) *MainWindow {
	mw := &MainWindow{window: win, pluginsVisible: true}

	mw.picker = NewPickerPanel(nil) // onSelect wired later by app
	mw.screen = NewScreenWidget()
	mw.navbar = NewNavBar(nil) // onKey wired later by app
	mw.logcat = NewLogcatPanel(store)
	mw.plugins = NewPluginsPanel(registry)
	mw.plugins.SetWindow(win)

	// Left side starts as the picker.
	mw.leftContent = mw.picker.Content()

	// Toggle button for the plugins column
	mw.toggleBtn = widget.NewButtonWithIcon("", theme.NavigateNextIcon(), mw.togglePlugins)
	mw.toggleBtn.Importance = widget.LowImportance

	term := terminal.New()
	go func() {
		for true {
			_ = term.RunLocalShell()
		}
	}()

	tabs := container.NewAppTabs(
		container.NewTabItem("Logcat", mw.logcat.Content()),
		container.NewTabItem("Terminal", term),
	)

	// Right side: logcat + (optional) plugins column
	rightSplit := container.NewHSplit(tabs, mw.plugins.Content())
	rightSplit.SetOffset(0.7)
	mw.rightSplit = rightSplit

	rightWithToggle := container.NewBorder(nil, nil, nil, mw.toggleBtn, rightSplit)
	mw.rightContent = rightWithToggle

	// HSplit: left = picker/emulator, right = logcat+plugins
	mw.split = container.NewHSplit(mw.leftContent, rightWithToggle)
	mw.split.SetOffset(0.45)

	win.SetContent(mw.split)
	win.Resize(fyne.NewSize(1400, 800))

	// Wire window for the package filter dialog
	mw.logcat.SetWindow(win)

	return mw
}

// Picker returns the picker panel.
func (mw *MainWindow) Picker() *PickerPanel { return mw.picker }

// Screen returns the screen widget (emulator or mirrored device).
func (mw *MainWindow) Screen() *ScreenWidget { return mw.screen }

// NavBar returns the navigation bar.
func (mw *MainWindow) NavBar() *NavBar { return mw.navbar }

// Plugins returns the plugins panel.
func (mw *MainWindow) Plugins() *PluginsPanel { return mw.plugins }

// togglePlugins shows/hides the plugins column.
// Note: Split panics if Trailing is set to nil; we Hide/Show the content
// object instead (Split checks Trailing.Visible()).
func (mw *MainWindow) togglePlugins() {
	mw.pluginsVisible = !mw.pluginsVisible
	content := mw.rightSplit.Trailing
	if content == nil {
		return
	}
	if mw.pluginsVisible {
		content.Show()
		mw.toggleBtn.SetIcon(theme.NavigateNextIcon())
	} else {
		content.Hide()
		mw.toggleBtn.SetIcon(theme.NavigateBackIcon())
	}
	mw.rightSplit.Refresh()
}

// Logcat returns the logcat panel.
func (mw *MainWindow) Logcat() *LogcatPanel { return mw.logcat }

// ShowScreen switches the left panel from the picker to the screen view:
// a bar with the name and the Stop (emulator) or Disconnect (device)
// button, the screen, and the navigation bar (back/home/recents) below.
func (mw *MainWindow) ShowScreen(title string, emulator bool) {
	bar := mw.emulatorOrDeviceBar(title, emulator)
	mw.split.Leading = container.NewBorder(bar, mw.navbar.Content(), nil, nil, mw.screen)
	mw.split.Refresh()
}

// ShowDevice switches the left panel from the picker to a connected
// device's summary, used when its screen can't be mirrored: note says why.
// Logs and plugins work anyway.
func (mw *MainWindow) ShowDevice(title string, details []string, note string) {
	bar := mw.emulatorOrDeviceBar(title, false)

	name := widget.NewLabel(title)
	name.TextStyle = fyne.TextStyle{Bold: true}
	name.Alignment = fyne.TextAlignCenter
	info := widget.NewLabel(strings.Join(details, "\n"))
	info.Alignment = fyne.TextAlignCenter
	noteLabel := widget.NewLabel(note + "\nLogs and plugins work as usual.")
	noteLabel.Alignment = fyne.TextAlignCenter
	noteLabel.SizeName = theme.SizeNameCaptionText

	body := container.NewCenter(container.NewVBox(name, info, widget.NewSeparator(), noteLabel))
	mw.split.Leading = container.NewBorder(bar, nil, nil, nil, body)
	mw.split.Refresh()
}

func (mw *MainWindow) emulatorOrDeviceBar(title string, emulator bool) fyne.CanvasObject {
	if emulator {
		return mw.sessionBar(title, "Stop", theme.MediaStopIcon())
	}
	return mw.sessionBar(title, "Disconnect", theme.LogoutIcon())
}

// sessionBar is the top bar of the emulator/device views.
func (mw *MainWindow) sessionBar(title, action string, icon fyne.Resource) fyne.CanvasObject {
	label := widget.NewLabel(title)
	label.TextStyle = fyne.TextStyle{Bold: true}
	label.Truncation = fyne.TextTruncateEllipsis
	btn := widget.NewButtonWithIcon(action, icon, func() {
		if mw.onDisconnect != nil {
			mw.onDisconnect()
		}
	})
	return container.NewBorder(nil, nil, nil, btn, label)
}

// SetOnDisconnect sets the action of the Stop (emulator) and Disconnect
// (device) buttons.
func (mw *MainWindow) SetOnDisconnect(f func()) {
	mw.onDisconnect = f
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
