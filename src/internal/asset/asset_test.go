package asset

import (
	"testing"

	"github.com/deese/wpkget/src/internal/github"
)

// Names carry no OS or architecture keywords so the result does not depend on
// the platform running the tests.
func assetsNamed(names ...string) []github.Asset {
	out := make([]github.Asset, len(names))
	for i, n := range names {
		out[i] = github.Asset{Name: n}
	}
	return out
}

func TestSelectPreferZip(t *testing.T) {
	tests := []struct {
		name      string
		assets    []github.Asset
		match     string
		preferZip bool
		want      string
	}{
		{"default keeps the first candidate", assetsNamed("tool.exe", "tool.zip"), "", false, "tool.exe"},
		{"prefer zip over a bare exe", assetsNamed("tool.exe", "tool.zip"), "", true, "tool.zip"},
		{"prefer zip regardless of order", assetsNamed("tool.zip", "tool.exe"), "", true, "tool.zip"},
		{"only an exe is still used", assetsNamed("tool.exe"), "", true, "tool.exe"},
		{"uppercase extension", assetsNamed("tool.exe", "TOOL.ZIP"), "", true, "TOOL.ZIP"},
		{"applies within a match pattern", assetsNamed("tool.exe", "tool.zip"), "tool*", true, "tool.zip"},
		{"never widens a match pattern", assetsNamed("tool.exe", "tool.zip"), "*.exe", true, "tool.exe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Select(tt.assets, "tool", tt.match, tt.preferZip, false)
			if err != nil {
				t.Fatalf("Select: %v", err)
			}
			if got.Name != tt.want {
				t.Errorf("selected %q, want %q", got.Name, tt.want)
			}
		})
	}
}

func TestFilterByToolchain(t *testing.T) {
	tests := []struct {
		name   string
		assets []github.Asset
		goos   string
		want   []string
	}{
		{"msvc beats gnu on windows", assetsNamed("fd-x86_64-pc-windows-gnu.zip", "fd-x86_64-pc-windows-msvc.zip"), "windows", []string{"fd-x86_64-pc-windows-msvc.zip"}},
		{"gnu is used when there is no msvc", assetsNamed("fd-x86_64-pc-windows-gnu.zip", "fd.zip"), "windows", []string{"fd-x86_64-pc-windows-gnu.zip"}},
		{"no flavor keeps nothing", assetsNamed("fd.zip"), "windows", nil},
		{"other platforms are untouched", assetsNamed("fd-gnu.tar.gz", "fd-msvc.tar.gz"), "linux", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterByToolchain(tt.assets, tt.goos)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d assets, want %d", len(got), len(tt.want))
			}
			for i, a := range got {
				if a.Name != tt.want[i] {
					t.Errorf("got %q, want %q", a.Name, tt.want[i])
				}
			}
		})
	}
}

func TestSelectMatchBypassesToolchain(t *testing.T) {
	assets := assetsNamed("fd-x86_64-pc-windows-gnu.zip", "fd-x86_64-pc-windows-msvc.zip")
	got, err := Select(assets, "fd", "*-gnu.zip", false, false)
	if err != nil || got.Name != "fd-x86_64-pc-windows-gnu.zip" {
		t.Fatalf("--match must override the precedence: %v %v", got, err)
	}
}
