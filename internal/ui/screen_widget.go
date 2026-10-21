package ui

import (
	"image"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/rmarkiewicz/android-companion/internal/emulator"
)

// PointerAction is the phase of a touch on the screen.
type PointerAction int

const (
	PointerDown PointerAction = iota
	PointerMove
	PointerUp
)

// ScreenKey is a non-text key forwarded to the device. Text (letters,
// digits, symbols, space) is forwarded as text instead.
type ScreenKey int

const (
	KeyEnter ScreenKey = iota + 1
	KeyBackspace
	KeyDelete
	KeyTab
	KeyEscape
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
)

var screenKeys = map[fyne.KeyName]ScreenKey{
	fyne.KeyReturn:    KeyEnter,
	fyne.KeyEnter:     KeyEnter,
	fyne.KeyBackspace: KeyBackspace,
	fyne.KeyDelete:    KeyDelete,
	fyne.KeyTab:       KeyTab,
	fyne.KeyEscape:    KeyEscape,
	fyne.KeyUp:        KeyUp,
	fyne.KeyDown:      KeyDown,
	fyne.KeyLeft:      KeyLeft,
	fyne.KeyRight:     KeyRight,
	fyne.KeyHome:      KeyHome,
	fyne.KeyEnd:       KeyEnd,
	fyne.KeyPageUp:    KeyPageUp,
	fyne.KeyPageDown:  KeyPageDown,
}

// ScreenInput receives the input on the screen. Positions are in pixels
// of the displayed frame, whose size is frameW x frameH.
type ScreenInput interface {
	Pointer(action PointerAction, x, y, frameW, frameH int)
	// Scroll: dx, dy are in wheel "clicks"; positive dy scrolls up.
	Scroll(x, y, frameW, frameH int, dx, dy float32)
	Key(k ScreenKey)
	Text(s string)
}

// scrollClick is the scroll delta (in Fyne units) of one wheel click.
const scrollClick = 10

// ScreenWidget shows a device or emulator screen and forwards mouse and
// keyboard input to it. Clicking it gives it the keyboard focus.
// All methods must be called from the UI goroutine.
type ScreenWidget struct {
	widget.BaseWidget

	img    *canvas.Image
	border *canvas.Rectangle

	frameW, frameH int
	input          ScreenInput

	pressed      bool
	lastX, lastY int
	focused      bool
}

var (
	_ desktop.Mouseable  = (*ScreenWidget)(nil)
	_ fyne.Draggable     = (*ScreenWidget)(nil)
	_ fyne.Scrollable    = (*ScreenWidget)(nil)
	_ fyne.Focusable     = (*ScreenWidget)(nil)
	_ fyne.Tabbable      = (*ScreenWidget)(nil)
	_ fyne.Shortcutable  = (*ScreenWidget)(nil)
	_ desktop.Cursorable = (*ScreenWidget)(nil)
)

// NewScreenWidget creates the widget.
func NewScreenWidget() *ScreenWidget {
	w := &ScreenWidget{}
	w.img = canvas.NewImageFromImage(image.NewRGBA(image.Rect(0, 0, 1, 1)))
	w.img.FillMode = canvas.ImageFillContain
	w.img.ScaleMode = canvas.ImageScaleFastest
	w.border = canvas.NewRectangle(color.Transparent)
	w.border.StrokeWidth = 2
	w.border.Hide()
	w.ExtendBaseWidget(w)
	return w
}

// SetInput sets where input goes (nil: nowhere).
func (w *ScreenWidget) SetInput(in ScreenInput) {
	w.input = in
	w.pressed = false
}

// SetImage shows img. It isn't copied: the caller must not modify it
// while it's shown.
func (w *ScreenWidget) SetImage(img *image.RGBA) {
	w.frameW, w.frameH = img.Rect.Dx(), img.Rect.Dy()
	w.img.Image = img
	w.img.Refresh()
}

// SetFrame shows an emulator frame (RGB888).
func (w *ScreenWidget) SetFrame(f emulator.Frame) {
	width, height := int(f.Width), int(f.Height)
	if width == 0 || height == 0 || len(f.Pixels) < width*height*3 {
		return
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	src := f.Pixels
	for y := 0; y < height; y++ {
		srcRow := src[y*width*3 : (y+1)*width*3]
		dstRow := img.Pix[y*img.Stride : y*img.Stride+width*4]
		for i, j := 0, 0; i < len(srcRow); i, j = i+3, j+4 {
			dstRow[j+0] = srcRow[i+0]
			dstRow[j+1] = srcRow[i+1]
			dstRow[j+2] = srcRow[i+2]
			dstRow[j+3] = 255
		}
	}
	w.SetImage(img)
}

// Clear drops the image, so a new session doesn't show the previous screen.
func (w *ScreenWidget) Clear() {
	w.frameW, w.frameH = 0, 0
	w.pressed = false
	w.img.Image = image.NewRGBA(image.Rect(0, 0, 1, 1))
	w.img.Refresh()
}

// MinSize returns a reasonable default size.
func (w *ScreenWidget) MinSize() fyne.Size {
	return fyne.NewSize(360, 640)
}

// CreateRenderer builds the renderer with the image.
func (w *ScreenWidget) CreateRenderer() fyne.WidgetRenderer {
	return &screenRenderer{w: w}
}

// --- Pointer ---

// MouseDown starts a touch (primary button only) and takes the focus.
func (w *ScreenWidget) MouseDown(e *desktop.MouseEvent) {
	if e.Button != desktop.MouseButtonPrimary {
		return
	}
	if c := fyne.CurrentApp().Driver().CanvasForObject(w); c != nil {
		c.Focus(w)
	}
	x, y, ok := w.toFrame(e.Position, false)
	if !ok || w.input == nil {
		return
	}
	w.pressed = true
	w.lastX, w.lastY = x, y
	w.input.Pointer(PointerDown, x, y, w.frameW, w.frameH)
}

// Dragged moves the touch; positions outside the frame are clamped.
func (w *ScreenWidget) Dragged(e *fyne.DragEvent) {
	if !w.pressed || w.input == nil {
		return
	}
	x, y, _ := w.toFrame(e.Position, true)
	if x == w.lastX && y == w.lastY {
		return
	}
	w.lastX, w.lastY = x, y
	w.input.Pointer(PointerMove, x, y, w.frameW, w.frameH)
}

// MouseUp ends the touch.
func (w *ScreenWidget) MouseUp(e *desktop.MouseEvent) {
	if !w.pressed || w.input == nil {
		return
	}
	x, y, _ := w.toFrame(e.Position, true)
	w.release(x, y)
}

// DragEnd ends the touch if MouseUp didn't (release outside the widget).
func (w *ScreenWidget) DragEnd() {
	if w.pressed && w.input != nil {
		w.release(w.lastX, w.lastY)
	}
}

func (w *ScreenWidget) release(x, y int) {
	w.pressed = false
	w.input.Pointer(PointerUp, x, y, w.frameW, w.frameH)
}

// Scrolled forwards the mouse wheel / trackpad.
func (w *ScreenWidget) Scrolled(e *fyne.ScrollEvent) {
	x, y, ok := w.toFrame(e.Position, false)
	if !ok || w.input == nil {
		return
	}
	w.input.Scroll(x, y, w.frameW, w.frameH, e.Scrolled.DX/scrollClick, e.Scrolled.DY/scrollClick)
}

// Cursor shows a crosshair over the screen.
func (w *ScreenWidget) Cursor() desktop.Cursor { return desktop.CrosshairCursor }

// toFrame converts a widget position to frame pixels. Outside the frame it
// returns ok=false, or the nearest edge pixel if clamp is set.
func (w *ScreenWidget) toFrame(pos fyne.Position, clamp bool) (x, y int, ok bool) {
	if w.frameW == 0 || w.frameH == 0 {
		return 0, 0, false
	}
	size := w.Size()
	fw, fh := float32(w.frameW), float32(w.frameH)
	scale := max(fw/size.Width, fh/size.Height) // frame pixels per widget unit
	offX := (size.Width - fw/scale) / 2
	offY := (size.Height - fh/scale) / 2

	x = int((pos.X - offX) * scale)
	y = int((pos.Y - offY) * scale)
	if x >= 0 && x < w.frameW && y >= 0 && y < w.frameH {
		return x, y, true
	}
	if !clamp {
		return 0, 0, false
	}
	return min(max(x, 0), w.frameW-1), min(max(y, 0), w.frameH-1), true
}

// --- Keyboard ---

// FocusGained shows the focus border.
func (w *ScreenWidget) FocusGained() {
	w.focused = true
	w.border.StrokeColor = theme.Color(theme.ColorNameFocus)
	w.border.Show()
	w.border.Refresh()
}

// FocusLost hides the focus border.
func (w *ScreenWidget) FocusLost() {
	w.focused = false
	w.border.Hide()
}

// TypedRune forwards text.
func (w *ScreenWidget) TypedRune(r rune) {
	if w.input != nil {
		w.input.Text(string(r))
	}
}

// TypedKey forwards non-text keys (Enter, Backspace, arrows...).
func (w *ScreenWidget) TypedKey(e *fyne.KeyEvent) {
	if k, ok := screenKeys[e.Name]; ok && w.input != nil {
		w.input.Key(k)
	}
}

// AcceptsTab sends Tab to the device instead of moving the focus.
func (w *ScreenWidget) AcceptsTab() bool { return true }

// TypedShortcut pastes the clipboard as text.
func (w *ScreenWidget) TypedShortcut(s fyne.Shortcut) {
	if paste, ok := s.(*fyne.ShortcutPaste); ok && w.input != nil && paste.Clipboard != nil {
		w.input.Text(paste.Clipboard.Content())
	}
}

type screenRenderer struct {
	w *ScreenWidget
}

func (r *screenRenderer) Layout(size fyne.Size) {
	r.w.img.Resize(size)
	r.w.border.Resize(size)
}

func (r *screenRenderer) MinSize() fyne.Size { return r.w.MinSize() }

func (r *screenRenderer) Refresh() {
	r.w.img.Refresh()
	r.w.border.Refresh()
}

func (r *screenRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.w.img, r.w.border}
}

func (r *screenRenderer) Destroy() {}
