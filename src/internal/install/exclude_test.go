package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deese/wpkget/src/internal/config"
)

func TestExcluded(t *testing.T) {
	defaults := config.DefaultExcludeFromAll
	tests := []struct {
		rel      string
		patterns []string
		want     bool
	}{
		{"LICENSE", defaults, true},
		{"license.txt", defaults, true}, // case-insensitive
		{"LICENSE-MIT", defaults, true},
		{"README.md", defaults, true},
		{"docs/README", defaults, true}, // any depth
		{"THIRD-PARTY-NOTICES", defaults, true},
		{"THIRD-PARTY-NOTICES/herdr-core.txt", defaults, true}, // a matching directory skips its subtree
		{"herdr-core.exe", defaults, false},
		{"conpty/conpty.dll", defaults, false},
		{"conpty/x64/OpenConsole.exe", []string{"conpty/x64"}, true}, // path pattern
		{"conpty/conpty.dll", []string{"conpty/x64"}, false},
		{"x64/OpenConsole.exe", []string{"conpty/x64"}, false}, // path patterns are anchored at the root
		{"anything", nil, false},
		{"anything", []string{""}, false},
	}
	for _, tt := range tests {
		if got := excluded(filepath.FromSlash(tt.rel), tt.patterns); got != tt.want {
			t.Errorf("excluded(%q, %v) = %v, want %v", tt.rel, tt.patterns, got, tt.want)
		}
	}
}

func TestValidateExclude(t *testing.T) {
	if err := validateExclude([]string{"LICENSE*", "conpty/x64"}); err != nil {
		t.Errorf("valid patterns rejected: %v", err)
	}
	if err := validateExclude([]string{"[unclosed"}); err == nil {
		t.Error("malformed pattern must be rejected")
	}
}

// coreFiles mimics the herdr-core zip: the executable, its conpty/ files and notices.
func coreFiles() map[string]string {
	return map[string]string{
		"herdr-core.exe":             "MZ-core",
		"conpty/conpty.dll":          "dll",
		"conpty/x64/OpenConsole.exe": "MZ-console",
		"LICENSE":                    "license",
		"THIRD-PARTY-NOTICES/a.txt":  "notice",
	}
}

// installAll runs extractAndCopyAll on a zip made of files and returns the
// relative slash paths that ended up in the destination.
func installAll(t *testing.T, files map[string]string, a allOptions) ([]string, string, error) {
	t.Helper()
	tmp, dest := t.TempDir(), t.TempDir()
	archive := filepath.Join(tmp, "asset.zip")
	if err := os.WriteFile(archive, zipBytes(t, files), 0o600); err != nil {
		t.Fatal(err)
	}
	err := extractAndCopyAll(archive, dest, tmp, a)

	var got []string
	_ = filepath.Walk(dest, func(p string, info os.FileInfo, werr error) error {
		if werr == nil && !info.IsDir() {
			rel, _ := filepath.Rel(dest, p)
			got = append(got, filepath.ToSlash(rel))
		}
		return nil
	})
	return got, dest, err
}

func contains(list []string, name string) bool {
	for _, s := range list {
		if s == name {
			return true
		}
	}
	return false
}

func TestExtractAllExcludesDefaults(t *testing.T) {
	got, _, err := installAll(t, coreFiles(), allOptions{exclude: config.DefaultExcludeFromAll, repoName: "core"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"herdr-core.exe", "conpty/conpty.dll", "conpty/x64/OpenConsole.exe"} {
		if !contains(got, want) {
			t.Errorf("%s missing from %v", want, got)
		}
	}
	for _, skipped := range []string{"LICENSE", "THIRD-PARTY-NOTICES/a.txt"} {
		if contains(got, skipped) {
			t.Errorf("%s must be excluded, got %v", skipped, got)
		}
	}
}

func TestExtractAllWithoutExclusions(t *testing.T) {
	got, _, err := installAll(t, coreFiles(), allOptions{repoName: "core"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(coreFiles()) {
		t.Errorf("an empty exclude list must copy everything, got %v", got)
	}
}

// --name with --all renames the main binary and still copies the rest.
func TestExtractAllRenamesMainBinary(t *testing.T) {
	got, dest, err := installAll(t, coreFiles(), allOptions{exclude: config.DefaultExcludeFromAll, rename: "hc.exe", repoName: "core"})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(got, "hc.exe") || contains(got, "herdr-core.exe") {
		t.Errorf("main binary must be installed as hc.exe only, got %v", got)
	}
	if !contains(got, "conpty/conpty.dll") || !contains(got, "conpty/x64/OpenConsole.exe") {
		t.Errorf("the rest of the archive must still be copied, got %v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "hc.exe")); string(b) != "MZ-core" {
		t.Errorf("hc.exe holds %q, want the herdr-core.exe content", b)
	}
}

// The renamed binary is the one findBinary picks for the repo name, not just any .exe.
func TestExtractAllRenamePicksTheRepoBinary(t *testing.T) {
	got, dest, err := installAll(t, coreFiles(), allOptions{rename: "hc.exe", repoName: "core"})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(got, "conpty/x64/OpenConsole.exe") {
		t.Errorf("OpenConsole.exe must keep its name, got %v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "hc.exe")); string(b) != "MZ-core" {
		t.Errorf("hc.exe holds %q, want the core executable", b)
	}
}

func TestExtractAllRefusesToExcludeMainBinary(t *testing.T) {
	_, _, err := installAll(t, coreFiles(), allOptions{exclude: []string{"herdr-*"}, rename: "hc.exe", repoName: "core"})
	if err == nil || !strings.Contains(err.Error(), "main binary") {
		t.Errorf("err = %v, want an error about the excluded main binary", err)
	}
}

func TestExtractAllRenameWithoutBinaryFails(t *testing.T) {
	_, _, err := installAll(t, map[string]string{"data.txt": "x"}, allOptions{rename: "hc.exe", repoName: "core"})
	if err == nil {
		t.Error("renaming with no executable in the archive must fail")
	}
}

func TestExtractAllRejectsBadPattern(t *testing.T) {
	_, _, err := installAll(t, coreFiles(), allOptions{exclude: []string{"[x"}, repoName: "core"})
	if err == nil {
		t.Error("malformed exclude pattern must fail the install")
	}
}
