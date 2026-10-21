package ui

import (
	"fmt"
	"image"
	"sync"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	"github.com/rmarkiewicz/android-companion/internal/emulator"
)

// EmulatorWidget displays the emulator screen and forwards pointer events.
type EmulatorWidget struct {
	widget.BaseWidget

	mu       sync.RWMutex
	frame    *image.RGBA
	frameW   int
	frameH   int
	hasFrame bool

	img *canvas.Image

	onTap     func(x, y int)
	onDrag    func(x, y int)
	onRelease func()

	framesReceived atomic.Int64
}

// NewEmulatorWidget creates the widget.
func NewEmulatorWidget() *EmulatorWidget {
	w := &EmulatorWidget{}
	w.img = canvas.NewImageFromImage(image.NewRGBA(image.Rect(0, 0, 1, 1)))
	w.img.FillMode = canvas.ImageFillContain
	w.img.ScaleMode = canvas.ImageScaleFastest
	w.ExtendBaseWidget(w)
	return w
}

// SetFrame stores a new frame (RGB888, bottom-up) and refreshes.
// Safe to call from any goroutine (but wrap in fyne.Do from the caller).
func (w *EmulatorWidget) SetFrame(f emulator.Frame) {
	n := w.framesReceived.Add(1)
	if n == 1 {
		fmt.Printf("[emulator] first frame: %dx%d (%d bytes)\n", f.Width, f.Height, len(f.Pixels))
	}
	if n%300 == 0 {
		fmt.Printf("[emulator] frames received: %d\n", n)
	}

	w.mu.Lock()
	w.frameW, w.frameH = int(f.Width), int(f.Height)
	if w.frameW == 0 || w.frameH == 0 || len(f.Pixels) == 0 {
		w.mu.Unlock()
		return
	}
	img := image.NewRGBA(image.Rect(0, 0, w.frameW, w.frameH))
	src := f.Pixels
	srcStride := w.frameW * 3
	dstStride := img.Stride
	// Row-by-row copy with manual RGB->RGBA expansion.
	// Faster than per-pixel loop; still not SIMD but acceptable at 900px width.
	for y := 0; y < w.frameH; y++ {
		srcRow := src[y*srcStride : (y+1)*srcStride]
		dstRow := img.Pix[y*dstStride : y*dstStride+w.frameW*4]
		j := 0
		for i := 0; i < len(srcRow); i += 3 {
			dstRow[j+0] = srcRow[i+0]
			dstRow[j+1] = srcRow[i+1]
			dstRow[j+2] = srcRow[i+2]
			dstRow[j+3] = 255
			j += 4
		}
	}
	w.frame = img
	w.hasFrame = true
	w.img.Image = img
	w.mu.Unlock()

	w.img.Refresh()
}

// Clear drops the last frame, so a new session doesn't show the previous
// emulator's screen.
func (w *EmulatorWidget) Clear() {
	w.mu.Lock()
	w.frame = nil
	w.hasFrame = false
	w.frameW, w.frameH = 0, 0
	w.img.Image = image.NewRGBA(image.Rect(0, 0, 1, 1))
	w.mu.Unlock()
	w.framesReceived.Store(0)
	w.img.Refresh()
}

// MinSize returns a reasonable default size.
func (w *EmulatorWidget) MinSize() fyne.Size {
	return fyne.NewSize(360, 640)
}

// CreateRenderer builds the renderer with the image.
func (w *EmulatorWidget) CreateRenderer() fyne.WidgetRenderer {
	return &emulatorRenderer{widget: w, img: w.img}
}

// --- Input handling ---

// Tapped handles tap/click.
func (w *EmulatorWidget) Tapped(e *fyne.PointEvent) {
	if w.onTap != nil {
		if x, y := w.toDeviceCoords(e.Position); x >= 0 {
			w.onTap(x, y)
		}
	}
}

// MouseDown starts a potential drag.
func (w *EmulatorWidget) MouseDown(e *fyne.PointEvent) {
	if w.onDrag != nil {
		if x, y := w.toDeviceCoords(e.Position); x >= 0 {
			w.onDrag(x, y)
		}
	}
}

// MouseUp ends a drag.
func (w *EmulatorWidget) MouseUp(e *fyne.PointEvent) {
	if w.onRelease != nil {
		w.onRelease()
	}
}

// Dragged handles drag motion.
func (w *EmulatorWidget) Dragged(e *fyne.DragEvent) {
	if w.onDrag != nil {
		if x, y := w.toDeviceCoords(e.Position); x >= 0 {
			w.onDrag(x, y)
		}
	}
}

// DragEnd ends a drag.
func (w *EmulatorWidget) DragEnd() {
	if w.onRelease != nil {
		w.onRelease()
	}
}

// toDeviceCoords converts widget coordinates to device pixel coordinates.
func (w *EmulatorWidget) toDeviceCoords(pos fyne.Position) (int, int) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if !w.hasFrame || w.frameW == 0 || w.frameH == 0 {
		return -1, -1
	}
	size := w.Size()
	fw, fh := float32(w.frameW), float32(w.frameH)
	scaleX := fw / size.Width
	scaleY := fh / size.Height
	scale := scaleX
	if scaleY > scaleX {
		scale = scaleY
	}
	offX := (size.Width - fw/scale) / 2
	offY := (size.Height - fh/scale) / 2

	sx := int((pos.X - offX) * scale)
	sy := int((pos.Y - offY) * scale)
	if sx < 0 || sx >= w.frameW || sy < 0 || sy >= w.frameH {
		return -1, -1
	}
	return sx, sy
}

// SetTapHandler sets the callback for taps.
func (w *EmulatorWidget) SetTapHandler(f func(x, y int)) { w.onTap = f }

// SetDragHandler sets the callback for drags.
func (w *EmulatorWidget) SetDragHandler(f func(x, y int)) { w.onDrag = f }

// SetReleaseHandler sets the callback for mouse/touch release.
func (w *EmulatorWidget) SetReleaseHandler(f func()) { w.onRelease = f }

// emulatorRenderer is the fyne renderer for EmulatorWidget.
type emulatorRenderer struct {
	widget *EmulatorWidget
	img    *canvas.Image
}

func (r *emulatorRenderer) Layout(size fyne.Size) {
	r.img.Resize(size)
}

func (r *emulatorRenderer) MinSize() fyne.Size {
	return r.widget.MinSize()
}

func (r *emulatorRenderer) Refresh() {
	r.img.Refresh()
}

func (r *emulatorRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.img}
}

func (r *emulatorRenderer) Destroy() {}
