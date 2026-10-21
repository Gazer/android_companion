package ui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/rmarkiewicz/android-companion/internal/logcat"
)

func TestMaxMessageWidth(t *testing.T) {
	test.NewApp()

	entries := []logcat.Entry{
		{Message: "short"},
		{Message: strings.Repeat("x", 200)},
	}
	w := maxMessageWidth(entries)
	// 200 monospace chars must be wider than a typical visible area
	if w < 600 {
		t.Errorf("maxMessageWidth = %.0f, want > 600 for 200 chars", w)
	}

	if got := maxMessageWidth(nil); got != 0 {
		t.Errorf("maxMessageWidth(nil) = %.0f, want 0", got)
	}
}

func TestFixedColumnsWidthConstant(t *testing.T) {
	if FixedColumnsWidth < 400 {
		t.Errorf("FixedColumnsWidth = %d, suspiciously small", FixedColumnsWidth)
	}
}
