package main

import (
	"strings"
	"testing"
)

func TestParseInstallList(t *testing.T) {
	in := `# tools
junegunn/fzf
BurntSushi/ripgrep --match "*msvc*.zip" --name rg  # trailing comment

sysinternals/autoruns -p sysinternals
neovim/neovim --all
`
	items, err := parseInstallList(strings.NewReader(in), "github")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("got %d items, want 4: %+v", len(items), items)
	}
	if items[0].arg != "junegunn/fzf" || items[0].provider != "github" {
		t.Errorf("item 0 = %+v", items[0])
	}
	if items[1].match != "*msvc*.zip" || items[1].binaryName != "rg" {
		t.Errorf("item 1 = %+v", items[1])
	}
	if items[2].provider != "sysinternals" {
		t.Errorf("item 2 = %+v", items[2])
	}
	if !items[3].all {
		t.Errorf("item 3 = %+v", items[3])
	}
}

func TestParseInstallListErrors(t *testing.T) {
	for _, in := range []string{
		"--all junegunn/fzf",
		"junegunn/fzf --bogus",
		"junegunn/fzf extra",
		`junegunn/fzf --match "unterminated`,
	} {
		if _, err := parseInstallList(strings.NewReader(in), ""); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}
