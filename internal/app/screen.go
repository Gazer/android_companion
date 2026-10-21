package app

import (
	"context"
	"time"

	"fyne.io/fyne/v2"

	"github.com/rmarkiewicz/android-companion/internal/adb"
	"github.com/rmarkiewicz/android-companion/internal/emulator"
	"github.com/rmarkiewicz/android-companion/internal/scrcpy"
	"github.com/rmarkiewicz/android-companion/internal/ui"
	"github.com/rmarkiewicz/android-companion/internal/video"
)

// Mirroring settings for physical devices.
const (
	// mirrorMaxSize limits the largest dimension of the mirrored frames:
	// sharp enough for the panel (also on HiDPI), cheap to decode.
	mirrorMaxSize = 1280
	mirrorMaxFPS  = 60
	// presentInterval is how often a new frame is shown, at most (~60 Hz).
	presentInterval = 16 * time.Millisecond
)

// screenBackend sends the screen widget's input to the emulator or the
// mirrored device. Methods are called from the UI goroutine and must not
// block.
type screenBackend interface {
	ui.ScreenInput
	Nav(k ui.NavKey)
	Close()
}

// --- Emulator (gRPC) ---

// Evdev key codes for the emulator's navigation keys.
const (
	evdevKeyBack      = 158 // KEY_BACK
	evdevKeyHome      = 172 // KEY_HOMEPAGE
	evdevKeyAppSwitch = 580 // KEY_APPSELECT (recents)
)

// W3C key names understood by the emulator for the screen keys.
var emulatorKeyNames = map[ui.ScreenKey]string{
	ui.KeyEnter:     "Enter",
	ui.KeyBackspace: "Backspace",
	ui.KeyDelete:    "Delete",
	ui.KeyTab:       "Tab",
	ui.KeyEscape:    "GoBack",
	ui.KeyUp:        "ArrowUp",
	ui.KeyDown:      "ArrowDown",
	ui.KeyLeft:      "ArrowLeft",
	ui.KeyRight:     "ArrowRight",
	ui.KeyHome:      "Home",
	ui.KeyEnd:       "End",
	ui.KeyPageUp:    "PageUp",
	ui.KeyPageDown:  "PageDown",
}

// emulatorScreen sends input over the emulator's gRPC API, in order, from
// a worker goroutine (gRPC calls block).
type emulatorScreen struct {
	emu    *emulator.Client
	queue  chan func(context.Context)
	ctx    context.Context
	cancel context.CancelFunc
}

func newEmulatorScreen(emu *emulator.Client) *emulatorScreen {
	ctx, cancel := context.WithCancel(context.Background())
	e := &emulatorScreen{emu: emu, queue: make(chan func(context.Context), 256), ctx: ctx, cancel: cancel}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case f := <-e.queue:
				f(ctx)
			}
		}
	}()
	return e
}

func (e *emulatorScreen) do(f func(context.Context)) {
	select {
	case e.queue <- f:
	default: // the emulator doesn't keep up: drop
	}
}

// Pointer sends the touch as a mouse with the primary button held. Frame
// coordinates are what the emulator expects (it scales them).
func (e *emulatorScreen) Pointer(action ui.PointerAction, x, y, _, _ int) {
	buttons := int32(1)
	if action == ui.PointerUp {
		buttons = 0
	}
	e.do(func(ctx context.Context) { _ = e.emu.SendMouse(ctx, int32(x), int32(y), buttons) })
}

// Scroll isn't supported for the emulator yet.
func (e *emulatorScreen) Scroll(int, int, int, int, float32, float32) {}

func (e *emulatorScreen) Key(k ui.ScreenKey) {
	if name, ok := emulatorKeyNames[k]; ok {
		e.do(func(ctx context.Context) { _ = e.emu.SendKeyName(ctx, name) })
	}
}

func (e *emulatorScreen) Text(s string) {
	e.do(func(ctx context.Context) { _ = e.emu.SendText(ctx, s) })
}

func (e *emulatorScreen) Nav(k ui.NavKey) {
	code := map[ui.NavKey]int32{
		ui.NavBack:    evdevKeyBack,
		ui.NavHome:    evdevKeyHome,
		ui.NavRecents: evdevKeyAppSwitch,
	}[k]
	e.do(func(ctx context.Context) { _ = e.emu.SendKey(ctx, code) })
}

func (e *emulatorScreen) Close() { e.cancel() }

// --- Physical device (scrcpy) ---

// Android key codes for the screen keys.
var androidKeycodes = map[ui.ScreenKey]uint32{
	ui.KeyEnter:     scrcpy.KeycodeEnter,
	ui.KeyBackspace: scrcpy.KeycodeDel,
	ui.KeyDelete:    scrcpy.KeycodeForwardDel,
	ui.KeyTab:       scrcpy.KeycodeTab,
	ui.KeyUp:        scrcpy.KeycodeDPadUp,
	ui.KeyDown:      scrcpy.KeycodeDPadDown,
	ui.KeyLeft:      scrcpy.KeycodeDPadLeft,
	ui.KeyRight:     scrcpy.KeycodeDPadRight,
	ui.KeyHome:      scrcpy.KeycodeMoveHome,
	ui.KeyEnd:       scrcpy.KeycodeMoveEnd,
	ui.KeyPageUp:    scrcpy.KeycodePageUp,
	ui.KeyPageDown:  scrcpy.KeycodePageDown,
}

// deviceScreen mirrors a physical device with scrcpy. Its input methods
// queue control messages (non-blocking).
type deviceScreen struct {
	sc *scrcpy.Session
}

func (d *deviceScreen) Pointer(action ui.PointerAction, x, y, w, h int) {
	a := map[ui.PointerAction]uint8{
		ui.PointerDown: scrcpy.MotionActionDown,
		ui.PointerMove: scrcpy.MotionActionMove,
		ui.PointerUp:   scrcpy.MotionActionUp,
	}[action]
	d.sc.Touch(a, x, y, w, h)
}

func (d *deviceScreen) Scroll(x, y, w, h int, dx, dy float32) {
	d.sc.Scroll(x, y, w, h, dx, dy)
}

func (d *deviceScreen) Key(k ui.ScreenKey) {
	if k == ui.KeyEscape {
		d.sc.Back()
		return
	}
	if code, ok := androidKeycodes[k]; ok {
		d.sc.Key(code)
	}
}

func (d *deviceScreen) Text(s string) { d.sc.Text(s) }

func (d *deviceScreen) Nav(k ui.NavKey) {
	switch k {
	case ui.NavBack:
		d.sc.Back()
	case ui.NavHome:
		d.sc.Key(scrcpy.KeycodeHome)
	case ui.NavRecents:
		d.sc.Key(scrcpy.KeycodeAppSwitch)
	}
}

func (d *deviceScreen) Close() { d.sc.Close() }

// startMirroring starts scrcpy on the device and streams its screen into
// the screen widget. On failure it returns why, and the caller shows the
// device summary instead.
func (s *session) startMirroring(ctx context.Context, serial, title string, details []string) error {
	dec, err := video.NewH264()
	if err != nil {
		return err
	}
	sc, err := scrcpy.Start(ctx, scrcpy.Options{
		AdbPath: s.mgr.AdbBin,
		Serial:  serial,
		MaxSize: mirrorMaxSize,
		MaxFPS:  mirrorMaxFPS,
	})
	if err != nil {
		dec.Close()
		return err
	}
	backend := &deviceScreen{sc: sc}
	s.mu.Lock()
	s.screen = backend
	s.mu.Unlock()

	fb := video.NewFrameBuffer()
	fyne.Do(func() {
		s.mw.Screen().Clear()
		s.mw.Screen().SetInput(backend)
	})
	go s.presentFrames(ctx, fb)
	go func() {
		err := decodeStream(sc, dec, fb)
		dec.Close()
		if ctx.Err() != nil {
			return // stopped by us
		}
		// If the device is gone, the logcat stream ends too and the
		// session goes back to the picker. Otherwise only mirroring
		// stopped: keep the logs and say why.
		if state, _ := adb.GetState(context.Background(), s.mgr.AdbBin, serial); state != adb.StateReady {
			return
		}
		s.mirroringStopped(ctx, backend, title, details, err)
	}()
	return nil
}

// decodeStream decodes the video packets until the stream ends.
func decodeStream(sc *scrcpy.Session, dec *video.Decoder, fb *video.FrameBuffer) error {
	var config []byte
	for {
		p, err := sc.ReadPacket()
		if err != nil {
			return err
		}
		switch {
		case p.Session:
			continue
		case p.Config:
			// Codec configuration (SPS/PPS): prepend it to the next frame.
			config = append(config[:0], p.Data...)
			continue
		}
		data := p.Data
		if len(config) > 0 {
			data = append(config, p.Data...)
			config = config[:0]
		}
		// A corrupt packet only affects the frames until the next key
		// frame: keep going.
		_ = dec.Decode(data, fb)
	}
}

// presentFrames shows the latest decoded frame, at most every
// presentInterval, until ctx is done.
func (s *session) presentFrames(ctx context.Context, fb *video.FrameBuffer) {
	ticker := time.NewTicker(presentInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fyne.Do(func() {
				// Take on the UI goroutine: the frame stays untouched
				// until the next Take.
				if img := fb.Take(); img != nil && ctx.Err() == nil {
					s.mw.Screen().SetImage(img)
				}
			})
		}
	}
}
