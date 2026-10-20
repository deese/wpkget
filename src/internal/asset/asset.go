package asset

import (
	"errors"
	"fmt"
	"log"
	"path"
	"runtime"
	"strings"

	"github.com/deese/wpkget/src/internal/github"
)

// ErrNoAsset is returned when no suitable asset is found for the release.
var ErrNoAsset = errors.New("no suitable asset found")

// allowedExts lists the file extensions wpkget can handle.
var allowedExts = []string{".zip", ".tar.gz", ".gz", ".exe"}

// Select picks the best asset for the current OS and architecture.
// When match is non-empty it is used as a glob pattern (see path.Match) to
// select candidates, skipping the OS/arch heuristics.
// Without match, heuristics are applied in order:
//  1. Filter to allowed extensions only.
//  2. Prefer assets that explicitly match the current OS.
//     If none match, fall back to excluding assets for other known platforms.
//  3. Among the OS candidates, prefer the current architecture.
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
		goos := runtime.GOOS
		goarch := runtime.GOARCH
		if osMatch := filterByOSPositive(candidates, goos); len(osMatch) > 0 {
			candidates = osMatch
		} else if noOther := filterByExcludingOtherPlatforms(candidates, goos); len(noOther) > 0 {
			candidates = noOther
		}
		if arch := filterByArch(candidates, goarch); len(arch) > 0 {
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

// matchesOS reports whether an asset name (lowercased) matches a given GOOS.
func matchesOS(nameLower, goos string) bool {
	switch goos {
	case "windows":
		// "darwin" contains "win" — require boundary tokens for Windows.
		return strings.Contains(nameLower, "windows") ||
			strings.Contains(nameLower, "win-") ||
			strings.Contains(nameLower, "win_") ||
			strings.Contains(nameLower, "-win.") ||
			strings.Contains(nameLower, "_win.")
	case "darwin":
		return strings.Contains(nameLower, "darwin") ||
			strings.Contains(nameLower, "macos") ||
			strings.Contains(nameLower, "osx") ||
			strings.Contains(nameLower, "apple")
	default:
		return strings.Contains(nameLower, goos)
	}
}

// filterByOSPositive keeps assets that explicitly match the current OS.
func filterByOSPositive(assets []github.Asset, goos string) []github.Asset {
	var out []github.Asset
	for _, a := range assets {
		if matchesOS(strings.ToLower(a.Name), goos) {
			out = append(out, a)
		}
	}
	return out
}

// knownGOOS is the set of platforms we can identify in asset names.
var knownGOOS = []string{"windows", "linux", "darwin", "freebsd", "android"}

// filterByExcludingOtherPlatforms removes assets that belong to a different OS.
func filterByExcludingOtherPlatforms(assets []github.Asset, goos string) []github.Asset {
	var out []github.Asset
	for _, a := range assets {
		lower := strings.ToLower(a.Name)
		excluded := false
		for _, other := range knownGOOS {
			if other != goos && matchesOS(lower, other) {
				excluded = true
				break
			}
		}
		// musl is Linux-specific; exclude when not on Linux.
		if !excluded && goos != "linux" && strings.Contains(lower, "musl") {
			excluded = true
		}
		if !excluded {
			out = append(out, a)
		}
	}
	return out
}

// archKeywords returns naming conventions used in release assets for goarch, in priority order.
func archKeywords(goarch string) []string {
	switch goarch {
	case "amd64":
		return []string{"amd64", "x86_64", "x64"}
	case "arm64":
		return []string{"arm64", "aarch64"}
	case "386":
		return []string{"386", "i386", "i686"}
	case "arm":
		return []string{"armv7", "armhf", "arm-"}
	default:
		return []string{goarch}
	}
}

func filterByArch(assets []github.Asset, goarch string) []github.Asset {
	for _, kw := range archKeywords(goarch) {
		if matches := filterByKeyword(assets, kw); len(matches) > 0 {
			return matches
		}
	}
	return nil
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
