// TODO: wire into CLI via --config flag
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	OutputDir   string `json:"output_dir"`
	Concurrency int    `json:"concurrency"`
	Quality     string `json:"quality"`
	FFmpegPath  string `json:"ffmpeg_path"`
	Proxy       string `json:"proxy"`
	UserAgent   string `json:"user_agent"`
}

func DefaultConfig() *Config {
	return &Config{
		OutputDir:   ".",
		Concurrency: 10,
		Quality:     "best",
	}
}

func configDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "medigo")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "medigo")
}

func Load() *Config {
	cfg := DefaultConfig()
	data, err := os.ReadFile(filepath.Join(configDir(), "config.json"))
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, cfg)
	return cfg
}
