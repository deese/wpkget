package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds all user-configurable settings for wpkget.
type Config struct {
	BinDir       string `yaml:"bin_dir"`
	ZipdownURL   string `yaml:"zipdown_url"`
	ZipdownToken string `yaml:"zipdown_token"`
	UserAgent    string `yaml:"user_agent"`
}

// Load reads the config file from the given path.
// If path is empty it falls back to WPKGET_CONFIG env var,
// then to %APPDATA%\wpkget\config.yaml.
// When using the default path and the file does not exist, it is created with
// default values so the user has a starting point to edit.
func Load(path string) (*Config, error) {
	cfg := defaults()

	resolved, isDefault := resolvePath(path)
	if resolved == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(resolved)
	if os.IsNotExist(err) {
		if isDefault {
			if err := writeDefaults(resolved, cfg); err != nil {
				return nil, err
			}
		}
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", resolved, err)
	}

	var overlay Config
	if err := yaml.Unmarshal(data, &overlay); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", resolved, err)
	}
	// Merge: only overwrite defaults with values the user explicitly set.
	if overlay.BinDir != "" {
		cfg.BinDir = overlay.BinDir
	}
	if overlay.ZipdownURL != "" {
		cfg.ZipdownURL = overlay.ZipdownURL
	}
	if overlay.ZipdownToken != "" {
		cfg.ZipdownToken = overlay.ZipdownToken
	}
	if overlay.UserAgent != "" {
		cfg.UserAgent = overlay.UserAgent
	}

	return cfg, nil
}

// writeDefaults creates the config file at path with commented default values.
func writeDefaults(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("config: create directory: %w", err)
	}

	content := "# wpkget configuration\n" +
		"# https://github.com/deese/wpkget\n\n" +
		"# Directory where binaries are installed.\n" +
		"bin_dir: " + cfg.BinDir + "\n\n" +
		"# zipdown service — leave empty to disable.\n" +
		"zipdown_url: \"\"\n" +
		"zipdown_token: \"\"\n\n" +
		"# User-Agent for HTTP requests.\n" +
		"user_agent: \"" + cfg.UserAgent + "\"\n"

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("config: write default config: %w", err)
	}
	return nil
}

// resolvePath returns the config file path to use and whether it is the
// auto-generated default (as opposed to an explicit user override).
func resolvePath(flag string) (path string, isDefault bool) {
	if flag != "" {
		return flag, false
	}
	if env := os.Getenv("WPKGET_CONFIG"); env != "" {
		return env, false
	}
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", false
	}
	return filepath.Join(appData, "wpkget", "config.yaml"), true
}

func defaults() *Config {
	appData := os.Getenv("APPDATA")
	return &Config{
		BinDir:    filepath.Join(appData, "wpkget", "bin"),
		UserAgent: "wpkget",
	}
}
