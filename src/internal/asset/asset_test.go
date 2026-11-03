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
