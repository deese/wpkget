package install

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/deese/wpkget/src/internal/asset"
	"github.com/deese/wpkget/src/internal/github"
	"github.com/deese/wpkget/src/internal/gitlab"
	"github.com/deese/wpkget/src/internal/sysinternals"
)

// Release lookups, replaceable in tests so Fetch can run without network access.
var (
	getLatest = latestRelease
	getByTag  = releaseByTag
)

// releaseByTag dispatches to the correct provider.
func releaseByTag(provider, host, repo, tag, userAgent string, debugHTTP bool) (*github.Release, error) {
	switch provider {
	case "gitlab":
		return gitlab.ReleaseByTag(host, repo, tag, userAgent, debugHTTP)
	case sysinternals.Provider:
		return sysinternals.ReleaseByTag(repo, tag, userAgent, debugHTTP)
	}
	return github.ReleaseByTag(repo, tag, userAgent, debugHTTP)
}

// resolveRelease returns the release to use: the one tagged opts.Tag, or the
// latest when no tag is requested. A tag that is not found is retried with the
// "v" prefix added or removed ("1.2.3" <-> "v1.2.3"), since users often omit it.
func resolveRelease(opts Options) (*github.Release, error) {
	if opts.Tag == "" {
		return getLatest(opts.Provider, opts.Host, opts.Repo, opts.UserAgent, opts.DebugHTTP)
	}

	byTag := func(tag string) (*github.Release, error) {
		return getByTag(opts.Provider, opts.Host, opts.Repo, tag, opts.UserAgent, opts.DebugHTTP)
	}

	release, err := byTag(opts.Tag)
	if err == nil || !errors.Is(err, github.ErrNotFound) {
		return release, err
	}
	alt := "v" + opts.Tag
	if strings.HasPrefix(opts.Tag, "v") {
		alt = strings.TrimPrefix(opts.Tag, "v")
	}
	if alt == "" || alt == opts.Tag {
		return nil, err
	}
	if release, altErr := byTag(alt); altErr == nil {
		return release, nil
	}
	return nil, fmt.Errorf("%w (also tried %q)", err, alt)
}

// pickAsset chooses the asset to download, logging the decision when verbose.
func pickAsset(opts Options, release *github.Release) (*github.Asset, error) {
	if opts.Verbose {
		log.Printf("release: %s %s (%s)", opts.Repo, release.TagName, sourceLabel(opts.Provider, opts.Host))
	}
	chosen, err := asset.Select(release.Assets, repoBaseName(opts.Repo), opts.Match, opts.Verbose)
	if err != nil {
		return nil, err
	}
	if opts.Verbose {
		log.Printf("asset: url %s", chosen.BrowserDownloadURL)
	}
	return chosen, nil
}

// downloadTimed downloads the asset and reports how large it was and how long it took.
func downloadTimed(opts Options, chosen *github.Asset, tmpDir string) (path string, size int64, dur time.Duration, err error) {
	start := time.Now()
	path, err = download(opts, chosen, tmpDir)
	if err != nil {
		return "", 0, 0, err
	}
	dur = time.Since(start)
	if info, statErr := os.Stat(path); statErr == nil {
		size = info.Size()
	}
	dprint(opts.Debug, "debug: downloaded archive → %s", path)
	return path, size, dur, nil
}

// Fetch downloads the release asset of opts.Repo (the latest release, or the
// one tagged opts.Tag) and leaves the result in opts.BinDir, the current
// directory when empty. Unlike Run it neither installs into a bin_dir nor is
// meant to be tracked: it is a one-off download.
//
// By default the selected binary is extracted and written as BinaryName (or
// the repo name). With All the whole archive is extracted into a
// "<repo>-<tag>" subdirectory, and with NoExtract the downloaded file is kept
// as is. Existing destinations are refused unless Force is set.
func Fetch(opts Options) (*Result, error) {
	opts = withProviderDefaults(opts)
	release, err := resolveRelease(opts)
	if err != nil {
		return nil, err
	}
	chosen, err := pickAsset(opts, release)
	if err != nil {
		return nil, err
	}

	repoName := repoBaseName(opts.Repo)
	outDir := opts.BinDir
	if outDir == "" {
		outDir = "."
	}
	dest := fetchDest(opts, outDir, repoName, release.TagName, chosen.Name)
	if abs, absErr := filepath.Abs(dest); absErr == nil {
		dest = abs
	}
	// Fail before downloading anything if the destination is already taken.
	if err := checkFree(dest, opts.All && !opts.NoExtract, opts.Force); err != nil {
		return nil, err
	}

	if opts.DryRun {
		if opts.Feedback == nil { // the modern output reports the dry run itself
			fmt.Printf("dry-run: would download %s %s\n", opts.Repo, release.TagName)
			fmt.Printf("dry-run: asset %s\n", chosen.Name)
			fmt.Printf("dry-run: url   %s\n", chosen.BrowserDownloadURL)
			fmt.Printf("dry-run: dest  %s\n", dest)
		}
		return &Result{Version: release.TagName, BinaryPath: dest}, nil
	}

	tmpDir, err := os.MkdirTemp(os.TempDir(), "wpkget-*")
	if err != nil {
		return nil, fmt.Errorf("install: create temp dir: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			log.Printf("warning: failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	archivePath, size, dur, err := downloadTimed(opts, chosen, tmpDir)
	if err != nil {
		return nil, err
	}

	result := &Result{Version: release.TagName, BinaryPath: dest, Size: size, Duration: dur}

	switch {
	case opts.NoExtract:
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, fmt.Errorf("install: create output dir: %w", err)
		}
		if err := moveFile(archivePath, dest); err != nil {
			return nil, fmt.Errorf("install: save download: %w", err)
		}
	case opts.All:
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return nil, fmt.Errorf("install: create output dir: %w", err)
		}
		if err := extractAndCopyAll(archivePath, dest, tmpDir, opts.Debug, opts.DebugHTTP); err != nil {
			return nil, err
		}
	default:
		binaryInTmp, err := extractBinary(archivePath, tmpDir, repoName, opts.DebugHTTP)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, fmt.Errorf("install: create output dir: %w", err)
		}
		if err := moveFile(binaryInTmp, dest); err != nil {
			return nil, fmt.Errorf("install: save binary: %w", err)
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(dest, 0o755); err != nil {
				return nil, fmt.Errorf("install: chmod binary: %w", err)
			}
		}
	}
	return result, nil
}

// fetchDest returns the path Fetch will create.
func fetchDest(opts Options, outDir, repoName, tag, assetName string) string {
	switch {
	case opts.NoExtract:
		return filepath.Join(outDir, assetName)
	case opts.All:
		return filepath.Join(outDir, sanitizeName(repoName+"-"+tag))
	default:
		return filepath.Join(outDir, resolveDestName(opts.BinaryName, repoName))
	}
}

// sanitizeName replaces characters that are not valid in file names.
func sanitizeName(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, s)
}

// checkFree reports an error if dest exists and force is not set. A directory
// destination (isDir) is accepted when it exists but is empty.
func checkFree(dest string, isDir, force bool) error {
	info, err := os.Stat(dest)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("install: check destination: %w", err)
	}
	if force {
		return nil
	}
	if isDir && info.IsDir() {
		entries, readErr := os.ReadDir(dest)
		if readErr == nil && len(entries) == 0 {
			return nil
		}
	}
	return fmt.Errorf("install: %s already exists (use --force to overwrite)", dest)
}
