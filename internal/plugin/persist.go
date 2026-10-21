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

// configFile is ~/.config/android-companion/charts.json (platform dir).
func configFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "android-companion")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "charts.json"), nil
}

// SaveCharts writes the chart configs to disk. Missing dir is created.
func SaveCharts(cfgs []ChartConfig) error {
	path, err := configFile()
	if err != nil {
		return err
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
func statusesFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "android-companion")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "statuses.json"), nil
}

// SaveStatuses writes status configs to disk.
func SaveStatuses(cfgs []StatusConfig) error {
	path, err := statusesFile()
	if err != nil {
		return err
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
