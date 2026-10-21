package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadCharts(t *testing.T) {
	// Redirect config dir to a temp location for isolation
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	// macOS ignores XDG_CONFIG_HOME; override via HOME too
	// (os.UserConfigDir on darwin = $HOME/Library/Application Support)
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	cfgs := []ChartConfig{
		{Name: "sr_perf_01", Pattern: "Session Replay telemetry: sr_perf_01={%d}ms"},
		{Name: "fps", Pattern: "fps: {%f}"},
	}
	if err := SaveCharts(cfgs); err != nil {
		t.Fatalf("SaveCharts failed: %v", err)
	}

	loaded, err := LoadCharts()
	if err != nil {
		t.Fatalf("LoadCharts failed: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded %d charts, want 2", len(loaded))
	}
	if loaded[0].Name != "sr_perf_01" || loaded[0].Pattern != cfgs[0].Pattern {
		t.Errorf("chart[0] = %+v, want %+v", loaded[0], cfgs[0])
	}
	if loaded[1].Name != "fps" {
		t.Errorf("chart[1].Name = %q, want fps", loaded[1].Name)
	}
}

func TestSaveAndLoadCounters(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	if loaded, err := LoadCounters(); err != nil || loaded != nil {
		t.Fatalf("LoadCounters with no file = %v, %v; want nil, nil", loaded, err)
	}
	cfgs := []CounterConfig{{Name: "crashes", Pattern: "FATAL EXCEPTION"}}
	if err := SaveCounters(cfgs); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadCounters()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0] != cfgs[0] {
		t.Fatalf("loaded %+v, want %+v", loaded, cfgs)
	}
}

func TestLoadChartsMissingFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	home := filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	loaded, err := LoadCharts()
	if err != nil {
		t.Fatalf("LoadCharts with no file should not error, got: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil, got %v", loaded)
	}
}
