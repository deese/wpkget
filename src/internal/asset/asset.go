package asset

import (
	"errors"
	"fmt"
	"log"
	"path"
	"strings"

	"github.com/deese/wpkget/src/internal/github"
)

// ErrNoAsset is returned when no suitable asset is found for the release.
var ErrNoAsset = errors.New("no suitable Windows asset found")

// allowedExts lists the file extensions wpkget can handle.
var allowedExts = []string{".zip", ".tar.gz", ".gz", ".exe"}

// Select picks the best Windows asset from a release's asset list.
// When match is non-empty it is used as a glob pattern (see path.Match) to
// select candidates, skipping the windows/arch heuristics.
// Without match, heuristics are applied in order:
//  1. Filter to allowed extensions only.
//  2. Prefer assets containing "windows" in the name.
//  3. Among those prefer amd64/x86_64 over 386/i386.
//
// If multiple candidates remain after all filters, the first is used and a
// warning is printed. If none remain, ErrNoAsset is returned.
func Select(assets []github.Asset, repoName string, match string, verbose bool) (*github.Asset, error) {
	candidates := filterByExt(assets)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w in %d total assets", ErrNoAsset, len(assets))
	}

	if match != "" {
		filtered, err := filterByGlob(candidates, match)
		if err != nil {
			return nil, fmt.Errorf("invalid --match pattern %q: %w", match, err)
		}
		if len(filtered) == 0 {
			return nil, fmt.Errorf("%w: no asset matched pattern %q", ErrNoAsset, match)
		}
		candidates = filtered
	} else {
		if windows := filterByKeyword(candidates, "windows"); len(windows) > 0 {
			candidates = windows
		}
		if arch := filterByArch(candidates); len(arch) > 0 {
			candidates = arch
		}
	}

	if len(candidates) > 1 && verbose {
		names := make([]string, len(candidates))
		for i, a := range candidates {
			names[i] = a.Name
		}
		log.Printf("warning: multiple matching assets, using %q (alternatives: %s)",
			candidates[0].Name, strings.Join(names[1:], ", "))
	}

	return &candidates[0], nil
}

func filterByExt(assets []github.Asset) []github.Asset {
	var out []github.Asset
	for _, a := range assets {
		if hasAllowedExt(a.Name) {
			out = append(out, a)
		}
	}
	return out
}

func hasAllowedExt(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range allowedExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func filterByKeyword(assets []github.Asset, keyword string) []github.Asset {
	var out []github.Asset
	for _, a := range assets {
		if strings.Contains(strings.ToLower(a.Name), keyword) {
			out = append(out, a)
		}
	}
	return out
}

func filterByGlob(assets []github.Asset, pattern string) ([]github.Asset, error) {
	var out []github.Asset
	for _, a := range assets {
		matched, err := path.Match(pattern, a.Name)
		if err != nil {
			return nil, err
		}
		if matched {
			out = append(out, a)
		}
	}
	return out, nil
}

func filterByArch(assets []github.Asset) []github.Asset {
	preferred := []string{"amd64", "x86_64"}
	for _, kw := range preferred {
		if matches := filterByKeyword(assets, kw); len(matches) > 0 {
			return matches
		}
	}
	return nil
}
