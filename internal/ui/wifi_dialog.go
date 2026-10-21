package ui

import (
	"net"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// WiFiActions run adb for the Wi-Fi dialog. They're called off the UI
// goroutine with the raw user input, and return a user-facing error.
type WiFiActions struct {
	Pair    func(addr, code string) error
	Connect func(addr string) error
}

// ShowWiFiDialog opens the "Pair device over Wi-Fi" dialog: wireless
// debugging pairing with a code (Android 11+), or a plain `adb connect`.
func ShowWiFiDialog(win fyne.Window, actions WiFiActions) {
	// Line breaks are explicit: wrapped labels don't size dialogs well.
	intro := widget.NewLabel("Requires Android 11 or later, on the same network as this computer.\n" +
		"On the device, open Developer options › Wireless debugging and turn it on.")

	pairAddr := widget.NewEntry()
	pairAddr.SetPlaceHolder("IP address & port, e.g. 192.168.1.20:37123")
	pairCode := widget.NewEntry()
	pairCode.SetPlaceHolder("Wi-Fi pairing code, e.g. 123456")
	connectAddr := widget.NewEntry()
	connectAddr.SetPlaceHolder("IP address & port, e.g. 192.168.1.20:41231")

	status := widget.NewLabel("")
	status.Truncation = fyne.TextTruncateEllipsis

	var pairBtn, connectBtn *widget.Button
	run := func(busy string, action func() error, done func()) {
		pairBtn.Disable()
		connectBtn.Disable()
		status.Importance = widget.MediumImportance
		status.SetText(busy)
		go func() {
			err := action()
			fyne.Do(func() {
				pairBtn.Enable()
				connectBtn.Enable()
				if err != nil {
					status.Importance = widget.DangerImportance
					status.SetText("Failed: " + err.Error())
					return
				}
				status.Importance = widget.SuccessImportance
				done()
			})
		}()
	}

	pairBtn = widget.NewButton("Pair", func() {
		addr, code := pairAddr.Text, pairCode.Text
		run("Pairing…", func() error { return actions.Pair(addr, code) }, func() {
			status.SetText("Paired. The device shows up in the list in a few seconds;\n" +
				"if it doesn't, connect to it below.")
			// The connect port differs from the pairing port, but the IP
			// address is the same.
			if host, _, err := net.SplitHostPort(strings.TrimSpace(addr)); err == nil && connectAddr.Text == "" {
				connectAddr.SetText(host + ":")
			}
		})
	})
	pairBtn.Importance = widget.HighImportance

	connectBtn = widget.NewButton("Connect", func() {
		addr := connectAddr.Text
		run("Connecting…", func() error { return actions.Connect(addr) }, func() {
			status.SetText("Connected. Select the device in the list.")
		})
	})

	help := func(s string) *widget.Label {
		l := widget.NewLabel(s)
		l.SizeName = theme.SizeNameCaptionText
		return l
	}
	heading := func(s string) *widget.Label {
		l := widget.NewLabel(s)
		l.TextStyle = fyne.TextStyle{Bold: true}
		return l
	}

	content := container.NewVBox(
		intro,
		widget.NewSeparator(),
		heading("Pair with a pairing code"),
		help("Tap “Pair device with pairing code” and enter the IP address & port\n"+
			"and the code it shows. You only need to pair once."),
		pairAddr,
		pairCode,
		pairBtn,
		widget.NewSeparator(),
		heading("Connect"),
		help("Already paired, or set up with adb tcpip? Enter the IP address & port\n"+
			"shown on the Wireless debugging screen."),
		connectAddr,
		connectBtn,
		status,
	)

	d := dialog.NewCustom("Pair device over Wi-Fi", "Close", content, win)
	d.Resize(fyne.NewSize(480, 0)) // height grows to fit the content
	d.Show()
}
