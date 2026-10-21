package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ChartConfig is the persistable configuration of a chart plugin.
type ChartConfig struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
}

// StatusRule mirrors status.Rule without importing it (plugin package is
// the base; concrete plugins import it, not vice versa).
type StatusRule struct {
	Pattern string `json:"pattern"`
	Label   string `json:"label"`
	Color   string `json:"color"`
}

// StatusConfig is the persistable configuration of a status plugin.
type StatusConfig struct {
	Name  string       `json:"name"`
	Rules []StatusRule `json:"rules"`
}

// CounterConfig is the persistable configuration of a counter plugin.
// The count itself isn't saved: it starts at 0 on every run.
type CounterConfig struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
}

// configPath returns the path of a config file in the app's config dir
// (e.g. ~/Library/Application Support/android-companion/<name> on macOS,
// ~/.config/android-companion/<name> on Linux), creating the dir.
func configPath(name string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "android-companion")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// configFile is the persistence file for chart plugins.
func configFile() (string, error) { return configPath("charts.json") }

// SaveCharts writes the chart configs to disk. Missing dir is created.
func SaveCharts(cfgs []ChartConfig) error {
	path, err := configFile()
	if err != nil {
		return err
	}
	if cfgs == nil {
		cfgs = []ChartConfig{} // write [] instead of null
	}
	data, err := json.MarshalIndent(cfgs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadCharts reads saved chart configs. Returns nil (no error) if the
// file does not exist (fresh install).
func LoadCharts() ([]ChartConfig, error) {
	path, err := configFile()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfgs []ChartConfig
	if err := json.Unmarshal(data, &cfgs); err != nil {
		return nil, err
	}
	return cfgs, nil
}

// statusesFile is the persistence file for status plugins.
func statusesFile() (string, error) { return configPath("statuses.json") }

// SaveStatuses writes status configs to disk.
func SaveStatuses(cfgs []StatusConfig) error {
	path, err := statusesFile()
	if err != nil {
		return err
	}
	if cfgs == nil {
		cfgs = []StatusConfig{} // write [] instead of null
	}
	data, err := json.MarshalIndent(cfgs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadStatuses reads saved status configs. Nil if file missing.
func LoadStatuses() ([]StatusConfig, error) {
	path, err := statusesFile()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfgs []StatusConfig
	if err := json.Unmarshal(data, &cfgs); err != nil {
		return nil, err
	}
	return cfgs, nil
}

// SaveCounters writes counter configs to disk.
func SaveCounters(cfgs []CounterConfig) error {
	path, err := configPath("counters.json")
	if err != nil {
		return err
	}
	if cfgs == nil {
		cfgs = []CounterConfig{} // write [] instead of null
	}
	data, err := json.MarshalIndent(cfgs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadCounters reads saved counter configs. Nil if the file is missing.
func LoadCounters() ([]CounterConfig, error) {
	path, err := configPath("counters.json")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfgs []CounterConfig
	if err := json.Unmarshal(data, &cfgs); err != nil {
		return nil, err
	}
	return cfgs, nil
}
