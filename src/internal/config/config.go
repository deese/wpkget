package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/deese/wpkget/src/internal/progress"

	"gopkg.in/yaml.v3"
)

// Config holds all user-configurable settings for wpkget.
type Config struct {
	BinDir       string `yaml:"bin_dir"`
	ZipdownURL   string `yaml:"zipdown_url"`
	ZipdownToken string `yaml:"zipdown_token"`
	UserAgent    string `yaml:"user_agent"`

	// GitLabHosts lists the self-hosted GitLab instances wpkget may talk to,
	// keyed by host (and port, if any), e.g. "git.example.com".
	GitLabHosts map[string]GitLabHost `yaml:"gitlab_hosts"`
	// GitHub holds the GitHub API credentials.
	GitHub GitHubConfig `yaml:"github"`
	// PassCmd is the pass-compatible binary used for token_pass lookups.
	// Defaults to "winpass" on Windows and "pass" elsewhere.
	PassCmd string `yaml:"pass_cmd"`
	// Progress controls the download progress bar: "auto" (terminals only),
	// "always" or "never". Overridden per run by --progress.
	Progress string `yaml:"progress"`
	// ModernTerm enables the colored output with aligned columns, a live
	// progress line and a summary (see internal/ui). Off by default.
	ModernTerm bool `yaml:"modern_term"`
}

// GitHubConfig holds the credentials used for api.github.com requests. A token
// lifts the anonymous rate limit (60 requests per hour). It is only ever sent
// to the GitHub API, never to asset downloads.
type GitHubConfig struct {
	// TokenEnv names an environment variable holding the token.
	// Defaults to GITHUB_TOKEN.
	TokenEnv string `yaml:"token_env"`
	// TokenPass is a pass/winpass entry whose first line is the token.
	// Used when the environment variable is unset or empty.
	TokenPass string `yaml:"token_pass"`
}

// GitLabHost holds the per-instance settings of a self-hosted GitLab.
// The token is only ever sent to the host it is configured for.
type GitLabHost struct {
	// Prefix is the URL path under which GitLab is served, e.g. "/gitlab" for
	// https://intranet.example.com/gitlab. Leave empty for root installs.
	Prefix string `yaml:"prefix"`
	// TokenEnv names an environment variable holding the access token.
	TokenEnv string `yaml:"token_env"`
	// TokenPass is a pass/winpass entry whose first line is the access token.
	// Used when TokenEnv is empty or the variable is unset.
	TokenPass string `yaml:"token_pass"`
	// CAFile is a PEM bundle trusted in addition to the system roots.
	// Environment variables ($VAR / ${VAR}) are expanded; when empty the
	// WPKGET_CA_FILE environment variable is used.
	CAFile string `yaml:"ca_file"`
	// SkipVerify disables TLS certificate verification. Prefer CAFile.
	SkipVerify bool `yaml:"skip_verify"`
}

// DefaultDir returns the platform-appropriate config directory for wpkget.
// Windows: %APPDATA%\wpkget
// Linux/Mac: $XDG_CONFIG_HOME/wpkget or ~/.config/wpkget
func DefaultDir() string {
	if appData := os.Getenv("APPDATA"); appData != "" {
		return filepath.Join(appData, "wpkget")
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "wpkget")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "wpkget")
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
	if overlay.PassCmd != "" {
		cfg.PassCmd = overlay.PassCmd
	}
	if overlay.ModernTerm {
		cfg.ModernTerm = true
	}
	if overlay.Progress != "" {
		if !progress.ValidMode(overlay.Progress) {
			return nil, fmt.Errorf("config: %s: invalid progress %q (want auto, always or never)", resolved, overlay.Progress)
		}
		cfg.Progress = overlay.Progress
	}
	cfg.GitLabHosts = overlay.GitLabHosts
	cfg.GitHub = overlay.GitHub

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
		"user_agent: \"" + cfg.UserAgent + "\"\n\n" +
		"# Download progress bar: auto (terminals only), always or never.\n" +
		"# Override per run with --progress.\n" +
		"progress: " + progress.Auto + "\n\n" +
		"# Modern terminal output: colors, aligned columns, live progress and a summary.\n" +
		"# Only used on terminals; override per run with --modern / --no-modern.\n" +
		"modern_term: false\n\n" +
		"# GitHub API token (optional): lifts the anonymous limit of 60 requests per hour.\n" +
		"# Read from the environment variable first, then from the pass entry (first line).\n" +
		"# github:\n" +
		"#   token_env: GITHUB_TOKEN             # default\n" +
		"#   token_pass: github/wpkget           # pass/winpass entry, see pass_cmd\n\n" +
		"# Self-hosted GitLab instances (optional). Example:\n" +
		"# gitlab_hosts:\n" +
		"#   git.example.com:\n" +
		"#     prefix: /gitlab                   # only if served under a subpath\n" +
		"#     token_env: GITLAB_TOKEN_EXAMPLE   # environment variable with the token\n" +
		"#     token_pass: gitlab/example        # or a pass/winpass entry (first line)\n" +
		"#     ca_file: ${EXAMPLE_CA_FILE}       # PEM bundle; env vars are expanded\n" +
		"#     skip_verify: false                # insecure; prefer ca_file\n" +
		"# pass_cmd: winpass                     # pass-compatible binary for token_pass\n"

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
	dir := DefaultDir()
	if dir == "" {
		return "", false
	}
	return filepath.Join(dir, "config.yaml"), true
}

func defaults() *Config {
	return &Config{
		BinDir:    defaultBinDir(),
		UserAgent: "wpkget",
		PassCmd:   defaultPassCmd(),
		Progress:  progress.Auto,
	}
}

// defaultPassCmd returns the pass-compatible binary for the platform.
func defaultPassCmd() string {
	if runtime.GOOS == "windows" {
		return "winpass"
	}
	return "pass"
}

// Path returns the effective config file path for the given --config value
// (flag, then WPKGET_CONFIG, then the platform default). It is empty when no
// location can be determined.
func Path(flag string) string {
	p, _ := resolvePath(flag)
	return p
}

// Ensure creates the config file at path with default values if it does not exist.
func Ensure(path string) error {
	_, err := os.Stat(path)
	if err == nil {
		return nil
	}
	if !os.IsNotExist(err) {
		return fmt.Errorf("config: stat %s: %w", path, err)
	}
	return writeDefaults(path, defaults())
}

// defaultBinDir returns the platform-appropriate default binary directory.
// Windows: %APPDATA%\wpkget\bin
// Linux/macOS: ~/.local/bin
func defaultBinDir() string {
	if appData := os.Getenv("APPDATA"); appData != "" {
		return filepath.Join(appData, "wpkget", "bin")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "bin")
}
