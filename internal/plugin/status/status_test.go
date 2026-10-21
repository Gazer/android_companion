package status

import (
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/rmarkiewicz/android-companion/internal/logcat"
)

func TestParseHexColor(t *testing.T) {
	tests := []struct {
		in      string
		wantErr bool
	}{
		{"#4CAF50", false},
		{"4CAF50", false},
		{"#fff", false},
		{"#FF000080", false},
		{"red", true},
		{"#12", true},
		{"", true},
	}
	for _, tt := range tests {
		_, err := ParseHexColor(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseHexColor(%q) err=%v, wantErr=%v", tt.in, err, tt.wantErr)
		}
	}
}

func TestStatusPluginStateTransitions(t *testing.T) {
	test.NewApp()

	rules := []Rule{
		{Pattern: "will stop the session replay", Label: "Stopped", Color: "#F44336"},
		{Pattern: "will start the session replay", Label: "Started", Color: "#4CAF50"},
	}
	p, err := New("replay", rules, func() {})
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// No match yet
	p.ProcessLine(logcat.Entry{Message: "unrelated line"})
	if p.active != -1 {
		t.Errorf("active = %d, want -1", p.active)
	}

	// First rule matches
	p.ProcessLine(logcat.Entry{Message: "Evaluation done, will start the session replay"})
	if p.active != 1 {
		t.Errorf("active = %d, want 1 (Started)", p.active)
	}

	// Second rule matches -> state switches
	p.ProcessLine(logcat.Entry{Message: "Evaluation done, will stop the session replay"})
	if p.active != 0 {
		t.Errorf("active = %d, want 0 (Stopped)", p.active)
	}

	// Non-matching line keeps state
	p.ProcessLine(logcat.Entry{Message: "something else"})
	if p.active != 0 {
		t.Errorf("active = %d, want 0 (unchanged)", p.active)
	}
}

func TestStatusPluginValidation(t *testing.T) {
	test.NewApp()

	if _, err := New("x", nil, func() {}); err == nil {
		t.Error("expected error for empty rules")
	}
	if _, err := New("x", []Rule{{Pattern: "a", Label: "b", Color: "nope"}}, func() {}); err == nil {
		t.Error("expected error for invalid color")
	}
	if _, err := New("x", []Rule{{Pattern: "", Label: "b", Color: "#fff"}}, func() {}); err == nil {
		t.Error("expected error for empty pattern")
	}
}

func TestContrastColor(t *testing.T) {
	white, _ := ParseHexColor("#FFFFFF")
	black, _ := ParseHexColor("#000000")

	c := contrastColor(white)
	r, _, _, _ := c.RGBA()
	if r != 0 {
		t.Error("white bg should get black text")
	}
	c = contrastColor(black)
	r, _, _, _ = c.RGBA()
	if r == 0 {
		t.Error("black bg should get white text")
	}
}
