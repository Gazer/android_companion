// Package app orchestrates the whole lifecycle: Fyne window, AVD picker,
// emulator launch, gRPC streaming, logcat storage, and shutdown.
package app

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/theme"

	"github.com/rmarkiewicz/android-companion/internal/avd"
	"github.com/rmarkiewicz/android-companion/internal/emulator"
	"github.com/rmarkiewicz/android-companion/internal/logcat"
	"github.com/rmarkiewicz/android-companion/internal/plugin"
	"github.com/rmarkiewicz/android-companion/internal/ui"
)

// Config controls startup behavior.
type Config struct {
	GRPCPort int
	AVDName  string
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
	// We create it without adb/serial for now; it's wired up after launch.
	logStore := logcat.NewStore("", "")

	// Plugin registry: receives every log line, renders widgets.
	registry := plugin.NewRegistry()

	mw := ui.NewMainWindow(win, logStore, registry)
	sess := &session{cfg: cfg, mgr: mgr, mw: mw, logStore: logStore, registry: registry}

	// Wire picker selection to launch
	mw.Picker().SetOnSelect(func(name string) {
		sess.launchAsync(ctx, name)
	})

	// Wire emulator input to gRPC
	mw.Emulator().SetTapHandler(func(x, y int) {
		sess.sendMouse(x, y, 1)
		sess.sendMouse(x, y, 0)
	})
	mw.Emulator().SetDragHandler(func(x, y int) {
		sess.sendMouse(x, y, 1)
	})
	mw.Emulator().SetReleaseHandler(func() {
		sess.sendMouseRelease()
	})

	// Wire navigation bar (back/home/recents) to gRPC sendKey
	mw.NavBar().SetOnKey(func(code int32) {
		sess.sendKey(code)
	})

	// Clean shutdown when window closes
	win.SetCloseIntercept(func() {
		sess.shutdown()
		cancel()
		win.Close()
	})

	// Load AVD list
	avds, err := mgr.ListAVDs(ctx)
	if err != nil {
		mw.Picker().SetStatus("Error listing AVDs: " + err.Error())
	} else if len(avds) == 0 {
		mw.Picker().SetStatus("No AVDs found. Create one in Android Studio first.")
	} else {
		mw.Picker().SetAVDs(avds)
		if cfg.AVDName != "" {
			sess.launchAsync(ctx, cfg.AVDName)
		}
	}

	win.ShowAndRun()
	return nil
}

// session holds per-emulator-run state.
type session struct {
	cfg      Config
	mgr      *avd.Manager
	mw       *ui.MainWindow
	logStore *logcat.Store
	registry *plugin.Registry

	mu        sync.Mutex
	running   bool
	launching bool
	inst      *avd.Instance
	emu       *emulator.Client
	streamer  *logcat.Streamer
	cancel    context.CancelFunc
	lastX     int
	lastY     int
}

func (s *session) launchAsync(parent context.Context, name string) {
	s.mu.Lock()
	if s.launching || s.running {
		s.mu.Unlock()
		return
	}
	s.launching = true
	s.mu.Unlock()

	fyne.Do(func() {
		s.mw.Picker().SetStatus(fmt.Sprintf("Launching %s...", name))
		s.mw.SetTitle("Android Companion — " + name)
	})

	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()

	go func() {
		inst, err := s.mgr.Launch(ctx, name, s.cfg.GRPCPort, s.cfg.WipeData)
		if err != nil {
			s.fail(fmt.Errorf("launch: %w", err))
			return
		}
		serial, err := inst.Serial(ctx)
		if err != nil {
			inst.Kill()
			s.fail(fmt.Errorf("serial: %w", err))
			return
		}
		emu, err := emulator.Connect(ctx, s.cfg.GRPCPort)
		if err != nil {
			inst.Kill()
			s.fail(fmt.Errorf("grpc: %w", err))
			return
		}
		// Wire the store to the device so it can resolve PIDs to packages
		s.logStore.SetDevice(s.mgr.AdbBin, serial)

		streamer := logcat.NewStreamer(s.mgr.AdbBin, serial)
		logCh, err := streamer.Start(ctx)
		if err != nil {
			emu.Close()
			inst.Kill()
			s.fail(fmt.Errorf("logcat: %w", err))
			return
		}

		s.mu.Lock()
		s.inst, s.emu, s.streamer = inst, emu, streamer
		s.running = true
		s.launching = false
		s.mu.Unlock()

		fyne.Do(func() {
			s.mw.ShowEmulator()
		})

		// Pump frames into UI, throttled to ~15fps
		go func() {
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
								s.mw.Emulator().SetFrame(f)
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
		}()

		// Pump logs into the store and dispatch to plugins (no UI update per log!)
		go func() {
			for e := range logCh {
				s.logStore.Add(e)
				s.registry.ProcessLine(e)
			}
		}()

		// Periodic UI refresh for the logcat panel (2fps is plenty for reading)
		go func() {
			ticker := time.NewTicker(500 * time.Millisecond)
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
	}()
}

func (s *session) fail(err error) {
	s.mu.Lock()
	s.launching = false
	s.mu.Unlock()
	fyne.Do(func() {
		s.mw.Picker().SetStatus("Error: " + err.Error())
		s.mw.SetTitle("Android Companion")
	})
}

func (s *session) sendMouse(x, y int, buttons int32) {
	s.mu.Lock()
	emu := s.emu
	s.lastX, s.lastY = x, y
	s.mu.Unlock()
	if emu != nil {
		_ = emu.SendMouse(context.Background(), int32(x), int32(y), buttons)
	}
}

func (s *session) sendMouseRelease() {
	s.mu.Lock()
	emu := s.emu
	x, y := s.lastX, s.lastY
	s.mu.Unlock()
	if emu != nil {
		_ = emu.SendMouse(context.Background(), int32(x), int32(y), 0)
	}
}

// sendKey forwards an evdev key code (navigation buttons) to the emulator.
func (s *session) sendKey(code int32) {
	s.mu.Lock()
	emu := s.emu
	s.mu.Unlock()
	if emu != nil {
		_ = emu.SendKey(context.Background(), code)
	}
}

// shutdown kills the emulator and stops all streams. Idempotent.
func (s *session) shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	if s.streamer != nil {
		s.streamer.Stop()
	}
	if s.emu != nil {
		s.emu.Close()
	}
	if s.inst != nil {
		s.inst.Kill()
	}
	s.running = false
	s.launching = false
}
