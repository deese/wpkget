package install

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// excluded reports whether rel, a path inside the extracted archive, matches
// one of the exclude_from_all patterns. Matching is case-insensitive.
//
// A pattern without "/" is compared with the name of every path component, so
// "LICENSE*" skips LICENSE files at any depth and a pattern that matches a
// directory skips its whole subtree. A pattern with "/" is compared with the
// path from the archive root, e.g. "conpty/x64".
func excluded(rel string, patterns []string) bool {
	comps := strings.Split(strings.ToLower(filepath.ToSlash(rel)), "/")
	for _, pat := range patterns {
		pat = strings.ToLower(strings.Trim(pat, "/"))
		if pat == "" {
			continue
		}
		for i, comp := range comps {
			subject := comp
			if strings.Contains(pat, "/") {
				subject = strings.Join(comps[:i+1], "/")
			}
			if ok, _ := path.Match(pat, subject); ok {
				return true
			}
		}
	}
	return false
}

// validateExclude rejects malformed patterns up front, so a typo in the config
// fails the install instead of silently excluding nothing.
func validateExclude(patterns []string) error {
	for _, pat := range patterns {
		if _, err := path.Match(pat, ""); err != nil {
			return fmt.Errorf("install: invalid exclude_from_all pattern %q: %w", pat, err)
		}
	}
	return nil
}
