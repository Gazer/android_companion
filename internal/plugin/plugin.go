// Package plugin defines the plugin interface for processing logcat lines.
// Plugins receive every parsed log line and can render custom widgets in
// the side panel.
package plugin

import (
	"fmt"
	"regexp"
	"strings"

	"fyne.io/fyne/v2"

	"github.com/rmarkiewicz/android-companion/internal/logcat"
)

// CompilePattern converts a user pattern with {%d} or {%f} placeholders
// into a regex with one capture group per placeholder.
// Example: "sr_perf_01={%d}ms" -> `sr_perf_01=(\d+)ms`.
// Everything else is quoted literally.
func CompilePattern(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	remaining := pattern
	for {
		i := strings.Index(remaining, "{%")
		if i < 0 {
			b.WriteString(regexp.QuoteMeta(remaining))
			break
		}
		b.WriteString(regexp.QuoteMeta(remaining[:i]))
		j := strings.Index(remaining[i:], "}")
		if j < 0 {
			return nil, fmt.Errorf("unclosed placeholder in pattern")
		}
		ph := remaining[i+2 : i+j]
		switch ph {
		case "d":
			b.WriteString(`(\d+)`)
		case "f":
			b.WriteString(`(\d+(?:\.\d+)?)`)
		case "s":
			b.WriteString(`(\S+)`)
		default:
			return nil, fmt.Errorf("unknown placeholder {%%%s}, use {%%d}, {%%f} or {%%s}", ph)
		}
		remaining = remaining[i+j+1:]
	}
	return regexp.Compile(b.String())
}

// Plugin processes logcat lines and renders a widget.
type Plugin interface {
	// Name returns the display name for the widget header.
	Name() string

	// ProcessLine is called for every new logcat entry (any goroutine).
	// Implementations must be goroutine-safe.
	ProcessLine(e logcat.Entry)

	// Widget returns the UI object rendered in the side panel.
	Widget() fyne.CanvasObject

	// Close releases resources (stops goroutines, etc).
	Close()
}

// Registry holds active plugins and dispatches log lines to them.
type Registry struct {
	plugins  []Plugin
	onAdd    func(p Plugin) // called on UI thread via fyne.Do by caller
	onRemove func(p Plugin) // for persistence
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// Add registers a plugin. onAdd callback fires (caller's responsibility
// to wrap in fyne.Do if needed).
func (r *Registry) Add(p Plugin) {
	r.plugins = append(r.plugins, p)
	if r.onAdd != nil {
		r.onAdd(p)
	}
}

// Remove unregisters and closes a plugin.
func (r *Registry) Remove(p Plugin) {
	for i, x := range r.plugins {
		if x == p {
			r.plugins = append(r.plugins[:i], r.plugins[i+1:]...)
			p.Close()
			if r.onRemove != nil {
				r.onRemove(p)
			}
			return
		}
	}
}

// ProcessLine broadcasts a line to all plugins.
func (r *Registry) ProcessLine(e logcat.Entry) {
	for _, p := range r.plugins {
		p.ProcessLine(e)
	}
}

// SetOnAdd sets the callback fired when a plugin is added.
func (r *Registry) SetOnAdd(f func(p Plugin)) {
	r.onAdd = f
}

// onRemove is called when a plugin is removed (for persistence).
func (r *Registry) SetOnRemove(f func(p Plugin)) {
	r.onRemove = f
}

// Plugins returns a snapshot of active plugins.
func (r *Registry) Plugins() []Plugin {
	cp := make([]Plugin, len(r.plugins))
	copy(cp, r.plugins)
	return cp
}
