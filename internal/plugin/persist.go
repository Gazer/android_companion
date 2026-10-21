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
