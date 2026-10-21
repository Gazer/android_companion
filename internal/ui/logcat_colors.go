package ui

import (
	"image/color"
	"hash/fnv"
)

// Color palette inspired by Android Studio logcat.
var (
	levelColors = map[string]color.RGBA{
		"V": {R: 0x88, G: 0x88, B: 0x88, A: 0xFF}, // gray
		"D": {R: 0x33, G: 0x99, B: 0xFF, A: 0xFF}, // blue
		"I": {R: 0x66, G: 0xCC, B: 0x66, A: 0xFF}, // green
		"W": {R: 0xFF, G: 0xBB, B: 0x33, A: 0xFF}, // amber
		"E": {R: 0xFF, G: 0x44, B: 0x44, A: 0xFF}, // red
		"F": {R: 0xFF, G: 0x22, B: 0x22, A: 0xFF}, // bright red
	}

	defaultTextColor = color.RGBA{R: 0xCC, G: 0xCC, B: 0xCC, A: 0xFF} // light gray
	metaColor        = color.RGBA{R: 0x99, G: 0x99, B: 0x99, A: 0xFF} // dimmer gray

	// Tag colors: warm, distinguishable palette
	tagPalette = []color.RGBA{
		{R: 0xE6, G: 0x5C, B: 0x7A, A: 0xFF}, // pinkish red
		{R: 0xE6, G: 0x8A, B: 0x5C, A: 0xFF}, // orange
		{R: 0xE6, G: 0xC8, B: 0x5C, A: 0xFF}, // yellow
		{R: 0x7A, G: 0xE6, B: 0x5C, A: 0xFF}, // lime
		{R: 0x5C, G: 0xE6, B: 0x8A, A: 0xFF}, // green
		{R: 0x5C, G: 0xE6, B: 0xC8, A: 0xFF}, // teal
		{R: 0x5C, G: 0xC8, B: 0xE6, A: 0xFF}, // cyan
		{R: 0x5C, G: 0x8A, B: 0xE6, A: 0xFF}, // blue
		{R: 0x7A, G: 0x5C, B: 0xE6, A: 0xFF}, // indigo
		{R: 0xC8, G: 0x5C, B: 0xE6, A: 0xFF}, // purple
		{R: 0xE6, G: 0x5C, B: 0xC8, A: 0xFF}, // magenta
		{R: 0xE6, G: 0x5C, B: 0x8A, A: 0xFF}, // rose
	}
)

// ColorForLevel returns the color for a log level (V/D/I/W/E/F).
func ColorForLevel(level string) color.RGBA {
	if c, ok := levelColors[level]; ok {
		return c
	}
	return defaultTextColor
}

// ColorForTag returns a deterministic color for a tag string.
// Same tag always gets the same color.
func ColorForTag(tag string) color.RGBA {
	if tag == "" {
		return metaColor
	}
	h := fnv.New32a()
	h.Write([]byte(tag))
	return tagPalette[int(h.Sum32())%len(tagPalette)]
}

// DefaultTextColor returns the color for the message body.
func DefaultTextColor() color.RGBA { return defaultTextColor }

// MetaColor returns the color for timestamps, PIDs, package names.
func MetaColor() color.RGBA { return metaColor }
