package packages

import (
	"path/filepath"
	"testing"
)

// A per-package exclude_from_all survives a save/load round trip, and an empty
// list (exclude nothing) stays distinct from an absent one (use the global list).
func TestExcludeFromAllRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "packages.yaml")
	custom, none := []string{"*.md", "docs"}, []string{}
	l := &List{path: path, Packages: []Entry{
		{Repo: "a/custom", Version: "1", ExcludeFromAll: &custom},
		{Repo: "a/none", Version: "1", ExcludeFromAll: &none},
		{Repo: "a/global", Version: "1"},
	}}
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if e := got.Get("a/custom").ExcludeFromAll; e == nil || len(*e) != 2 || (*e)[0] != "*.md" {
		t.Errorf("custom list lost: %v", e)
	}
	if e := got.Get("a/none").ExcludeFromAll; e == nil || len(*e) != 0 {
		t.Errorf("empty list must stay set and empty, got %v", e)
	}
	if e := got.Get("a/global").ExcludeFromAll; e != nil {
		t.Errorf("absent list must stay nil, got %v", *e)
	}
}
