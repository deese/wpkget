package packages

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Entry represents a single tracked package.
type Entry struct {
	Repo       string `yaml:"repo"`
	Version    string `yaml:"version"`
	BinaryName string `yaml:"binary_name,omitempty"` // optional custom name for the installed .exe
	Match      string `yaml:"match,omitempty"`        // optional glob pattern used to select the release asset
	All        bool   `yaml:"all,omitempty"`           // when true, all archive contents are copied to bin_dir
}

// List is the in-memory representation of packages.yaml.
type List struct {
	Packages []Entry `yaml:"packages"`
	path     string
}

// Load reads the package list from path. If the file does not exist an empty
// list is returned (not an error).
func Load(path string) (*List, error) {
	l := &List{path: path}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return l, nil
	}
	if err != nil {
		return nil, fmt.Errorf("packages: read %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, l); err != nil {
		return nil, fmt.Errorf("packages: parse %s: %w", path, err)
	}

	return l, nil
}

// Upsert adds a new entry or updates the version of an existing one.
// binaryName and match are stored only when non-empty; existing values are
// preserved when the argument is empty (i.e. update does not clear them).
// all is always stored as-is.
func (l *List) Upsert(repo, version, binaryName, match string, all bool) {
	for i, e := range l.Packages {
		if e.Repo == repo {
			l.Packages[i].Version = version
			if binaryName != "" {
				l.Packages[i].BinaryName = binaryName
			}
			if match != "" {
				l.Packages[i].Match = match
			}
			l.Packages[i].All = all
			return
		}
	}
	l.Packages = append(l.Packages, Entry{Repo: repo, Version: version, BinaryName: binaryName, Match: match, All: all})
}

// Remove deletes the entry for repo. Returns true if it was present.
func (l *List) Remove(repo string) bool {
	for i, e := range l.Packages {
		if e.Repo == repo {
			l.Packages = append(l.Packages[:i], l.Packages[i+1:]...)
			return true
		}
	}
	return false
}

// Get returns the entry for repo, or nil if not tracked.
func (l *List) Get(repo string) *Entry {
	for i, e := range l.Packages {
		if e.Repo == repo {
			return &l.Packages[i]
		}
	}
	return nil
}

// Save writes the list back to disk. It creates the parent directory if needed.
func (l *List) Save() error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("packages: create dir: %w", err)
	}

	data, err := yaml.Marshal(l)
	if err != nil {
		return fmt.Errorf("packages: marshal: %w", err)
	}

	if err := os.WriteFile(l.path, data, 0o644); err != nil {
		return fmt.Errorf("packages: write %s: %w", l.path, err)
	}

	return nil
}
