package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// vList is a minimal virtualized list with fixed row height, designed to
// live inside a container.Scroll with ScrollBoth (unlike widget.List,
// which owns its vertical scroller and swallows horizontal gestures).
//
// It renders only the visible rows (plus a small buffer) by positioning
// them absolutely inside a tall "content" container whose height equals
// rowHeight * itemCount.
type vList struct {
	widget.BaseWidget

	rowHeight float32
	contentW  float32 // full width reported via MinSize (horizontal scroll)

	// length returns the number of rows.
	length func() int
	// createRow makes a new reusable row object.
	createRow func() fyne.CanvasObject
	// updateRow binds row i to a (possibly reused) row object.
	updateRow func(i int, obj fyne.CanvasObject)

	content *fyne.Container // absolute-positioned rows
	pool    []fyne.CanvasObject
	top     int // first rendered row index
}

func newVList(rowHeight float32, length func() int, create func() fyne.CanvasObject, update func(int, fyne.CanvasObject)) *vList {
	v := &vList{
		rowHeight: rowHeight,
		length:    length,
		createRow: create,
		updateRow: update,
		top:       -1,
	}
	v.content = container.NewWithoutLayout()
	v.ExtendBaseWidget(v)
	return v
}

// setContentWidth updates the reported full content width.
func (v *vList) setContentWidth(w float32) {
	if w != v.contentW {
		v.contentW = w
		v.Refresh()
	}
}

// RefreshRows re-binds visible rows (call when data changes).
func (v *vList) RefreshRows() {
	v.renderRows(v.scrollY())
	v.BaseWidget.Refresh()
}

// scrollY reads the current vertical offset from the parent Scroll, if any.
func (v *vList) scrollY() float32 {
	// Our content is positioned by the Scroll at negative Y when scrolled.
	return -v.content.Position().Y
}

// OnScrolled is wired by the parent to re-render on scroll.
func (v *vList) OnScrolled(pos fyne.Position) {
	v.renderRows(pos.Y)
}

func (v *vList) renderRows(offsetY float32) {
	n := v.length()
	if n == 0 {
		v.content.Objects = nil
		v.content.Refresh()
		return
	}
	viewH := v.Size().Height
	first := int(offsetY / v.rowHeight)
	if first < 0 {
		first = 0
	}
	visible := int(viewH/v.rowHeight) + 2 // buffer
	last := first + visible
	if last > n {
		last = n
	}
	if first == v.top && len(v.content.Objects) == last-first {
		return // same window, keep pooled rows
	}
	v.top = first

	// Grow pool if needed
	for len(v.pool) < last-first {
		v.pool = append(v.pool, v.createRow())
	}

	width := v.contentW
	if width < v.Size().Width {
		width = v.Size().Width
	}

	objs := make([]fyne.CanvasObject, 0, last-first)
	for i := first; i < last; i++ {
		row := v.pool[i-first]
		v.updateRow(i, row)
		row.Move(fyne.NewPos(0, float32(i)*v.rowHeight))
		row.Resize(fyne.NewSize(width, v.rowHeight))
		row.Show()
		objs = append(objs, row)
	}
	v.content.Objects = objs
	v.content.Refresh()
}

// MinSize reports the full content size so the outer Scroll can scroll
// in both directions.
func (v *vList) MinSize() fyne.Size {
	h := float32(v.length()) * v.rowHeight
	w := v.contentW
	if w < 100 {
		w = 100
	}
	return fyne.NewSize(w, h)
}

func (v *vList) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(v.content)
}

// ScrollToBottom moves the parent scroll to the end.
func (v *vList) ScrollToBottom(s *container.Scroll) {
	s.ScrollToBottom()
	v.renderRows(v.scrollY())
}

var _ = canvas.Line{}
