// Package app orchestrates the whole lifecycle: Fyne window, device picker,
// emulator launch and gRPC streaming, logcat storage, and shutdown.
package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/theme"

	"github.com/rmarkiewicz/android-companion/internal/adb"
	"github.com/rmarkiewicz/android-companion/internal/avd"
	"github.com/rmarkiewicz/android-companion/internal/emulator"
	"github.com/rmarkiewicz/android-companion/internal/logcat"
	"github.com/rmarkiewicz/android-companion/internal/plugin"
	"github.com/rmarkiewicz/android-companion/internal/ui"
)

// devicePollInterval is how often the picker's device list is refreshed.
const devicePollInterval = 2 * time.Second

// Config controls startup behavior.
type Config struct {
	GRPCPort int
	AVDName  string // launch this AVD on start
	Serial   string // open this connected device on start
	WipeData bool
}

// Run starts the application and blocks until the window is closed.
func Run(parentCtx context.Context, cfg Config) error {
	mgr, err := avd.NewManager()
	if err != nil {
		return err
	}

	a := fyneapp.NewWithID("com.github.rmarkiewicz.androidcompanion")
	a.Settings().SetTheme(theme.DarkTheme())
	win := a.NewWindow("Android Companion")

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	// Shared log store: decoupled from UI, bounded in memory.
	// It's wired to a device when a session starts.
	logStore := logcat.NewStore("", "")

	// Plugin registry: receives every log line, renders widgets.
	registry := plugin.NewRegistry()

	mw := ui.NewMainWindow(win, logStore, registry)
	sess := &session{
		cfg:      cfg,
		mgr:      mgr,
		mw:       mw,
		logStore: logStore,
		registry: registry,
		pollNow:  make(chan struct{}, 1),
	}

	mw.Picker().SetOnSelect(func(t ui.Target) {
		sess.open(ctx, t)
	})
	mw.Picker().SetOnWiFi(func() {
		ui.ShowWiFiDialog(win, ui.WiFiActions{
			Pair:    func(addr, code string) error { return sess.pair(ctx, addr, code) },
			Connect: func(addr string) error { return sess.connect(ctx, addr) },
		})
	})
	mw.SetOnDisconnect(func() {
		go sess.stop("")
	})

	// The screen widget's input goes to the session's backend (set when a
	// session starts); the navigation bar too.
	mw.NavBar().SetOnKey(func(k ui.NavKey) {
		if b := sess.currentScreen(); b != nil {
			b.Nav(k)
		}
	})

	// Clean shutdown when window closes: stops the streams and kills the
	// emulator we launched (devices are left alone).
	win.SetCloseIntercept(func() {
		sess.shutdown()
		cancel()
		win.Close()
	})

	sess.loadAVDs(ctx)
	go sess.watchDevices(ctx)

	switch {
	case cfg.Serial != "":
		sess.open(ctx, ui.Target{Kind: ui.TargetDevice, ID: cfg.Serial, Title: cfg.Serial, Ready: true})
	case cfg.AVDName != "":
		sess.open(ctx, ui.Target{Kind: ui.TargetAVD, ID: cfg.AVDName, Title: cfg.AVDName, Ready: true})
	}

	win.ShowAndRun()
	return nil
}

// session holds the state of the current emulator or device session.
type session struct {
	cfg      Config
	mgr      *avd.Manager
	mw       *ui.MainWindow
	logStore *logcat.Store
	registry *plugin.Registry
	// pollNow asks watchDevices for an immediate refresh.
	pollNow chan struct{}

	mu sync.Mutex
	// busy is true while a session is starting or running.
	busy     bool
	inst     *avd.Instance // only for emulators we launched
	emu      *emulator.Client
	screen   screenBackend // nil when the screen isn't shown
	streamer *logcat.Streamer
	cancel   context.CancelFunc
	// lastDevice identifies the device whose logs are in the store.
	lastDevice string
}

func (s *session) currentScreen() screenBackend {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.screen
}

// loadAVDs fills the picker's emulator section.
func (s *session) loadAVDs(ctx context.Context) {
	var targets []ui.Target
	note := ""
	if !s.mgr.HasEmulator() {
		note = "Android Emulator not installed"
	} else if names, err := s.mgr.ListAVDs(ctx); err != nil {
		note = "Error listing AVDs: " + err.Error()
	} else if len(names) == 0 {
		note = "No AVDs: create one in Android Studio's Device Manager"
	} else {
		for _, name := range names {
			targets = append(targets, ui.Target{
				Kind:   ui.TargetAVD,
				ID:     name,
				Title:  strings.ReplaceAll(name, "_", " "),
				Detail: "Launches here, with its screen",
				Ready:  true,
			})
		}
	}
	s.mw.Picker().SetAVDs(targets, note)
}

// watchDevices keeps the picker's device list up to date while no session
// is running.
func (s *session) watchDevices(ctx context.Context) {
	ticker := time.NewTicker(devicePollInterval)
	defer ticker.Stop()
	avdNames := make(map[string]string) // running emulator serial -> AVD name
	for {
		if !s.isBusy() {
			devs, err := adb.ListDevices(ctx, s.mgr.AdbBin)
			targets, note := s.deviceTargets(ctx, devs, err, avdNames)
			fyne.Do(func() {
				s.mw.Picker().SetDevices(targets, note)
			})
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.pollNow:
		}
	}
}

func (s *session) pokeDevices() {
	select {
	case s.pollNow <- struct{}{}:
	default:
	}
}

func (s *session) deviceTargets(ctx context.Context, devs []adb.Device, err error, avdNames map[string]string) ([]ui.Target, string) {
	if err != nil {
		return nil, err.Error()
	}
	// Forget emulators that are gone: another AVD may reuse the serial.
	for serial := range avdNames {
		if !slices.ContainsFunc(devs, func(d adb.Device) bool { return d.Serial == serial }) {
			delete(avdNames, serial)
		}
	}
	var targets []ui.Target
	for _, d := range devs {
		t := ui.Target{
			Kind:   ui.TargetDevice,
			ID:     d.Serial,
			Title:  d.Name(),
			Detail: connectionDetail(d),
			Ready:  d.Ready(),
		}
		if d.IsEmulator() && d.Ready() {
			name, ok := avdNames[d.Serial]
			if !ok {
				name, _ = adb.EmulatorAVDName(ctx, s.mgr.AdbBin, d.Serial)
				avdNames[d.Serial] = name
			}
			if name != "" {
				t.Title = strings.ReplaceAll(name, "_", " ")
			}
		}
		targets = append(targets, t)
	}
	if len(targets) == 0 {
		return nil, "No devices: enable USB debugging and plug one in, or pair one over Wi-Fi"
	}
	return targets, ""
}

// connectionDetail describes how a device is connected, or why it can't
// be used.
func connectionDetail(d adb.Device) string {
	switch {
	case !d.Ready():
		return d.StateHint()
	case d.IsEmulator():
		return "Running emulator · " + d.Serial + " · logs only"
	case d.IsNetwork():
		return "Wi-Fi · " + d.Serial
	default:
		return "USB · " + d.Serial
	}
}

func (s *session) isBusy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy
}

// open starts a session: launches an AVD, or connects to a device.
func (s *session) open(parent context.Context, t ui.Target) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return
	}
	s.busy = true
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.mu.Unlock()

	verb := "Connecting to"
	if t.Kind == ui.TargetAVD {
		verb = "Launching"
	}
	fyne.Do(func() {
		s.mw.Picker().SetStatus(fmt.Sprintf("%s %s...", verb, t.Title))
		s.mw.SetTitle("Android Companion — " + t.Title)
	})

	go func() {
		var err error
		if t.Kind == ui.TargetAVD {
			err = s.launchEmulator(ctx, t)
		} else {
			err = s.connectDevice(ctx, t)
		}
		if err != nil {
			s.shutdown()
			s.fail(err)
		}
	}()
}

func (s *session) launchEmulator(ctx context.Context, t ui.Target) error {
	inst, err := s.mgr.Launch(ctx, t.ID, s.cfg.GRPCPort, s.cfg.WipeData)
	if err != nil {
		return fmt.Errorf("launch: %w", err)
	}
	s.mu.Lock()
	s.inst = inst
	s.mu.Unlock()

	emu, err := emulator.Connect(ctx, s.cfg.GRPCPort)
	if err != nil {
		return fmt.Errorf("grpc: %w", err)
	}
	backend := newEmulatorScreen(emu)
	s.mu.Lock()
	s.emu = emu
	s.screen = backend
	s.mu.Unlock()

	if err := s.startLogs(ctx, inst.Serial, "avd:"+t.ID, t.Title); err != nil {
		return err
	}
	fyne.Do(func() {
		s.mw.Screen().Clear()
		s.mw.Screen().SetInput(backend)
		s.mw.ShowScreen(t.Title, true)
	})
	go s.pumpFrames(ctx, emu)
	return nil
}

func (s *session) connectDevice(ctx context.Context, t ui.Target) error {
	info, err := adb.GetInfo(ctx, s.mgr.AdbBin, t.ID)
	if err != nil {
		return fmt.Errorf("%s: %w", t.ID, err)
	}
	title := info.Name()
	if title == "" {
		title = t.Title
	}
	details := []string{connectionDetail(adb.Device{Serial: t.ID, State: adb.StateReady})}
	if v := info.Version(); v != "" {
		details = append([]string{v}, details...)
	}

	if err := s.startLogs(ctx, t.ID, t.ID, title); err != nil {
		return err
	}
	mirrorErr := s.startMirroring(ctx, t.ID, title, details)
	fyne.Do(func() {
		s.mw.SetTitle("Android Companion — " + title)
		if mirrorErr != nil {
			s.mw.ShowDevice(title, details, "Screen mirroring unavailable: "+mirrorErr.Error())
		} else {
			s.mw.ShowScreen(title, false)
		}
	})
	return nil
}

// mirroringStopped shows the device summary when mirroring stops while the
// device is still connected (e.g. the scrcpy server crashed).
func (s *session) mirroringStopped(ctx context.Context, backend screenBackend, title string, details []string, err error) {
	s.mu.Lock()
	if s.screen != backend {
		s.mu.Unlock()
		return
	}
	s.screen = nil
	s.mu.Unlock()
	backend.Close()
	fyne.Do(func() {
		if ctx.Err() != nil {
			return
		}
		s.mw.Screen().SetInput(nil)
		s.mw.ShowDevice(title, details, "Screen mirroring stopped: "+err.Error())
	})
}

// startLogs streams the device's logcat into the store and the plugins.
// Logs of a previous session are kept if it was the same device (same
// key: the serial, or the AVD name for emulators, whose serial depends on
// the free port).
func (s *session) startLogs(ctx context.Context, serial, key, title string) error {
	s.mu.Lock()
	newDevice := key != s.lastDevice
	s.lastDevice = key
	s.mu.Unlock()

	if newDevice {
		s.logStore.Clear()
	}
	// Wire the store to the device so it can resolve PIDs to packages
	s.logStore.SetDevice(s.mgr.AdbBin, serial)

	streamer := logcat.NewStreamer(s.mgr.AdbBin, serial)
	logCh, err := streamer.Start(ctx)
	if err != nil {
		return fmt.Errorf("logcat: %w", err)
	}
	s.mu.Lock()
	s.streamer = streamer
	s.mu.Unlock()

	if newDevice {
		fyne.Do(func() {
			s.mw.Logcat().ResetDevice()
		})
	}

	// Pump logs into the store and dispatch to plugins (no UI update per log!)
	go func() {
		for e := range logCh {
			s.logStore.Add(e)
			s.registry.ProcessLine(e)
		}
		// adb logcat exited without us stopping it: the device went away
		// (unplugged, rebooted, emulator crashed...).
		if ctx.Err() == nil {
			s.stop(title + " disconnected")
		}
	}()

	// Periodic UI refresh for the logcat panel. Each tick only reads the
	// logs added since the previous one, so it's cheap.
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fyne.Do(func() {
					s.mw.Logcat().RefreshDisplay()
				})
			}
		}
	}()
	return nil
}

// pumpFrames streams the emulator screen into the UI, throttled to ~15fps.
func (s *session) pumpFrames(ctx context.Context, emu *emulator.Client) {
	var lastFrame emulator.Frame
	var hasPending atomic.Bool
	var frameMu sync.Mutex

	go func() {
		ticker := time.NewTicker(66 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				frameMu.Lock()
				if hasPending.Load() {
					f := lastFrame
					hasPending.Store(false)
					frameMu.Unlock()
					fyne.Do(func() {
						s.mw.Screen().SetFrame(f)
					})
				} else {
					frameMu.Unlock()
				}
			}
		}
	}()

	_ = emu.StreamFrames(ctx, func(f emulator.Frame) {
		frameMu.Lock()
		lastFrame = f
		hasPending.Store(true)
		frameMu.Unlock()
	})
}

// stop ends the session and goes back to the picker, showing reason.
func (s *session) stop(reason string) {
	s.shutdown()
	fyne.Do(func() {
		s.mw.Screen().SetInput(nil)
		s.mw.Screen().Clear()
		s.mw.ShowPicker()
		s.mw.SetTitle("Android Companion")
		s.mw.Picker().SetStatus(reason)
	})
	s.pokeDevices()
}

func (s *session) fail(err error) {
	fyne.Do(func() {
		s.mw.Picker().SetStatus("Error: " + err.Error())
		s.mw.SetTitle("Android Companion")
	})
	s.pokeDevices()
}

// pair pairs a device over Wi-Fi (wireless debugging pairing code).
func (s *session) pair(ctx context.Context, addr, code string) error {
	hostPort, err := adb.NormalizeAddress(addr, "")
	if err != nil {
		return err
	}
	code = strings.TrimSpace(code)
	if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		return errors.New("the pairing code has 6 digits")
	}
	if err := adb.Pair(ctx, s.mgr.AdbBin, hostPort, code); err != nil {
		return err
	}
	s.pokeDevices()
	return nil
}

// connect connects to a device over TCP/IP (`adb connect`).
func (s *session) connect(ctx context.Context, addr string) error {
	hostPort, err := adb.NormalizeAddress(addr, "5555")
	if err != nil {
		return err
	}
	if err := adb.Connect(ctx, s.mgr.AdbBin, hostPort); err != nil {
		return err
	}
	s.pokeDevices()
	return nil
}

// shutdown stops the streams and kills the emulator we launched, if any.
// Physical devices are never touched. Idempotent.
func (s *session) shutdown() {
	s.mu.Lock()
	cancel, streamer, screen, emu, inst := s.cancel, s.streamer, s.screen, s.emu, s.inst
	s.cancel, s.streamer, s.screen, s.emu, s.inst = nil, nil, nil, nil, nil
	s.busy = false
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if streamer != nil {
		streamer.Stop()
	}
	if screen != nil {
		screen.Close()
	}
	if emu != nil {
		emu.Close()
	}
	if inst != nil {
		_ = inst.Kill()
	}
}
