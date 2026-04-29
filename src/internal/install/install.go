package install

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/deese/wpkget/src/internal/asset"
	"github.com/deese/wpkget/src/internal/github"
	"github.com/deese/wpkget/src/internal/zipdown"
)

// Options controls a single install run.
type Options struct {
	Repo       string
	BinDir     string
	BinaryName string          // optional: rename the installed .exe to this name (without extension)
	Match      string          // optional: glob pattern to select the release asset (e.g. "*windows-amd64*.zip")
	All        bool            // when true, copy all archive contents to BinDir instead of extracting a single .exe
	DryRun     bool
	Verbose    bool
	Debug      bool            // enable step-by-step diagnostic output
	Zipdown    *zipdown.Client // nil means zipdown is disabled
}

// Result is returned by a successful Run.
type Result struct {
	Version    string
	BinaryPath string // final location of the installed binary
}

var httpClient = &http.Client{Timeout: 120 * time.Second}

// Run executes the full install pipeline for a single repository:
// resolve → download → decompress → move → cleanup.
func Run(opts Options) (*Result, error) {
	release, err := github.LatestRelease(opts.Repo)
	if err != nil {
		return nil, err
	}

	repoName := repoBaseName(opts.Repo)
	chosen, err := asset.Select(release.Assets, repoName, opts.Match, opts.Verbose)
	if err != nil {
		return nil, err
	}

	destName := resolveDestName(opts.BinaryName, repoName)

	if opts.DryRun {
		fmt.Printf("dry-run: would install %s %s\n", opts.Repo, release.TagName)
		fmt.Printf("dry-run: asset      %s\n", chosen.Name)
		fmt.Printf("dry-run: url        %s\n", chosen.BrowserDownloadURL)
		if opts.All {
			fmt.Printf("dry-run: dest (all) %s\n", opts.BinDir)
		} else {
			fmt.Printf("dry-run: dest       %s\n", filepath.Join(opts.BinDir, destName))
		}
		return &Result{Version: release.TagName}, nil
	}

	// Create a temporary working directory.
	tmpDir, err := os.MkdirTemp(os.TempDir(), "wpkget-*")
	if err != nil {
		return nil, fmt.Errorf("install: create temp dir: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			log.Printf("warning: failed to remove temp dir %s: %v", tmpDir, err)
		}
	}()

	// Download the asset (or let zipdown wrap a bare .exe).
	archivePath, err := download(opts, chosen, tmpDir)
	if err != nil {
		return nil, err
	}

	// Ensure the destination directory exists.
	if err := os.MkdirAll(opts.BinDir, 0o755); err != nil {
		return nil, fmt.Errorf("install: create bin dir: %w", err)
	}

	if opts.All {
		// Copy all archive contents to BinDir.
		if err := extractAndCopyAll(archivePath, opts.BinDir, tmpDir, opts.Debug); err != nil {
			return nil, err
		}
		if opts.Verbose {
			log.Printf("installed all files from %s %s → %s", opts.Repo, release.TagName, opts.BinDir)
		}
		return &Result{Version: release.TagName, BinaryPath: opts.BinDir}, nil
	}

	// Locate the single binary inside the archive (or use directly for bare .exe).
	binaryInTmp, err := extractBinary(archivePath, tmpDir, repoName)
	if err != nil {
		return nil, err
	}

	dest := filepath.Join(opts.BinDir, destName)
	if err := moveFile(binaryInTmp, dest); err != nil {
		return nil, fmt.Errorf("install: move binary: %w", err)
	}

	if opts.Verbose {
		log.Printf("installed %s %s → %s", opts.Repo, release.TagName, dest)
	}

	return &Result{Version: release.TagName, BinaryPath: dest}, nil
}

// ResolveURL returns the download URL for the latest Windows release asset
// without downloading anything.
func ResolveURL(repo string, match string, verbose bool) (string, string, error) {
	release, err := github.LatestRelease(repo)
	if err != nil {
		return "", "", err
	}
	chosen, err := asset.Select(release.Assets, repoBaseName(repo), match, verbose)
	if err != nil {
		return "", "", err
	}
	return release.TagName, chosen.BrowserDownloadURL, nil
}

// download fetches the asset to tmpDir and returns the local path.
// For bare .exe assets it first tries zipdown; if zipdown is not configured
// it downloads the .exe directly.
func download(opts Options, chosen *github.Asset, tmpDir string) (string, error) {
	isBareExe := strings.ToLower(filepath.Ext(chosen.Name)) == ".exe"

	if isBareExe && opts.Zipdown != nil {
		path, err := opts.Zipdown.Wrap(chosen.BrowserDownloadURL, tmpDir)
		if err == nil {
			return path, nil
		}
		if !errors.Is(err, zipdown.ErrNotConfigured) {
			return "", fmt.Errorf("install: zipdown: %w", err)
		}
		// Fall through to direct download.
	}

	return downloadURL(chosen.BrowserDownloadURL, tmpDir, chosen.Name)
}

// downloadURL streams a URL to destDir/<name> and returns the full path.
func downloadURL(url, destDir, name string) (string, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("install: download %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("install: download returned status %d", resp.StatusCode)
	}

	dest := filepath.Join(destDir, name)
	f, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("install: create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("install: write download: %w", err)
	}

	return dest, nil
}

// extractBinary decompresses archivePath into a subdirectory of tmpDir and
// returns the path to the selected .exe.
// If archivePath is itself a .exe it is returned as-is.
func extractBinary(archivePath, tmpDir, repoName string) (string, error) {
	lower := strings.ToLower(archivePath)

	switch {
	case strings.HasSuffix(lower, ".exe"):
		return archivePath, nil

	case strings.HasSuffix(lower, ".zip"):
		extractDir := filepath.Join(tmpDir, "extracted")
		if err := extractZip(archivePath, extractDir); err != nil {
			return "", err
		}
		return findBinary(extractDir, repoName)

	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		extractDir := filepath.Join(tmpDir, "extracted")
		if err := extractTarGz(archivePath, extractDir); err != nil {
			return "", err
		}
		return findBinary(extractDir, repoName)

	case strings.HasSuffix(lower, ".gz"):
		// Single file gzip — decompress to a .exe.
		outPath := strings.TrimSuffix(archivePath, ".gz")
		if !strings.HasSuffix(strings.ToLower(outPath), ".exe") {
			outPath += ".exe"
		}
		if err := extractGz(archivePath, outPath); err != nil {
			return "", err
		}
		return outPath, nil

	default:
		return "", fmt.Errorf("install: unrecognised archive format: %s", filepath.Base(archivePath))
	}
}

// findBinary walks dir and returns the path of the best-matching .exe.
func findBinary(dir, repoName string) (string, error) {
	var exes []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.ToLower(filepath.Ext(path)) == ".exe" {
			exes = append(exes, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("install: walk extracted dir: %w", err)
	}

	if len(exes) == 0 {
		return "", fmt.Errorf("install: no .exe found in archive")
	}
	if len(exes) == 1 {
		return exes[0], nil
	}

	// Prefer the one whose base name contains the repo name.
	for _, p := range exes {
		if strings.Contains(strings.ToLower(filepath.Base(p)), strings.ToLower(repoName)) {
			return p, nil
		}
	}

	// Fall back to the first one and warn.
	log.Printf("warning: multiple .exe files found, using %s", filepath.Base(exes[0]))
	return exes[0], nil
}

// extractZip unpacks a .zip archive into destDir.
func extractZip(src, destDir string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("install: open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if err := extractZipEntry(f, destDir); err != nil {
			return err
		}
	}
	return nil
}

func extractZipEntry(f *zip.File, destDir string) error {
	// Guard against zip-slip.
	target := filepath.Join(destDir, filepath.FromSlash(f.Name))
	if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
		return fmt.Errorf("install: zip-slip detected in entry %q", f.Name)
	}

	if f.FileInfo().IsDir() {
		return os.MkdirAll(target, 0o755)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("install: create dir for zip entry: %w", err)
	}

	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("install: open zip entry %q: %w", f.Name, err)
	}
	defer rc.Close()

	out, err := os.Create(target)
	if err != nil {
		return fmt.Errorf("install: create zip entry %q: %w", f.Name, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, rc); err != nil {
		return fmt.Errorf("install: write zip entry %q: %w", f.Name, err)
	}
	return nil
}

// extractTarGz unpacks a .tar.gz archive into destDir.
func extractTarGz(src, destDir string) error {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("install: open tar.gz: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("install: gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("install: read tar entry: %w", err)
		}
		if err := extractTarEntry(hdr, tr, destDir); err != nil {
			return err
		}
	}
	return nil
}

func extractTarEntry(hdr *tar.Header, r io.Reader, destDir string) error {
	target := filepath.Join(destDir, filepath.FromSlash(hdr.Name))
	if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
		return fmt.Errorf("install: tar-slip detected in entry %q", hdr.Name)
	}

	switch hdr.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, 0o755)

	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("install: create dir for tar entry: %w", err)
		}
		out, err := os.Create(target)
		if err != nil {
			return fmt.Errorf("install: create tar entry %q: %w", hdr.Name, err)
		}
		defer out.Close()
		if _, err := io.Copy(out, r); err != nil {
			return fmt.Errorf("install: write tar entry %q: %w", hdr.Name, err)
		}
	}
	return nil
}

// extractGz decompresses a single-file .gz to dest.
func extractGz(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("install: open gz: %w", err)
	}
	defer in.Close()

	gz, err := gzip.NewReader(in)
	if err != nil {
		return fmt.Errorf("install: gzip reader: %w", err)
	}
	defer gz.Close()

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("install: create decompressed file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, gz); err != nil {
		return fmt.Errorf("install: decompress gz: %w", err)
	}
	return nil
}

// extractAndCopyAll extracts archivePath and copies all its contents into destDir.
// For single-file .gz and bare .exe assets only that file is copied.
func extractAndCopyAll(archivePath, destDir, tmpDir string, debug bool) error {
	lower := strings.ToLower(archivePath)

	switch {
	case strings.HasSuffix(lower, ".exe"):
		dst := filepath.Join(destDir, filepath.Base(archivePath))
		if debug {
			log.Printf("debug: bare exe → %s", dst)
		}
		return copyFile(archivePath, dst)

	case strings.HasSuffix(lower, ".zip"):
		if debug {
			debugListZip(archivePath)
		}
		extractDir := filepath.Join(tmpDir, "extracted")
		if err := extractZip(archivePath, extractDir); err != nil {
			return err
		}
		if debug {
			debugWalkDir("extracted", extractDir)
		}
		src := unwrapSingleDir(extractDir)
		if debug {
			if src != extractDir {
				log.Printf("debug: unwrapped single top-level dir → %s", filepath.Base(src))
			} else {
				log.Printf("debug: no single top-level dir to unwrap, copying from %s", extractDir)
			}
		}
		return copyDirContents(src, destDir, debug)

	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		extractDir := filepath.Join(tmpDir, "extracted")
		if err := extractTarGz(archivePath, extractDir); err != nil {
			return err
		}
		if debug {
			debugWalkDir("extracted", extractDir)
		}
		src := unwrapSingleDir(extractDir)
		if debug {
			if src != extractDir {
				log.Printf("debug: unwrapped single top-level dir → %s", filepath.Base(src))
			} else {
				log.Printf("debug: no single top-level dir to unwrap, copying from %s", extractDir)
			}
		}
		return copyDirContents(src, destDir, debug)

	case strings.HasSuffix(lower, ".gz"):
		outPath := strings.TrimSuffix(archivePath, ".gz")
		if !strings.HasSuffix(strings.ToLower(outPath), ".exe") {
			outPath += ".exe"
		}
		if err := extractGz(archivePath, outPath); err != nil {
			return err
		}
		dst := filepath.Join(destDir, filepath.Base(outPath))
		if debug {
			log.Printf("debug: gz → %s", dst)
		}
		return copyFile(outPath, dst)

	default:
		return fmt.Errorf("install: unrecognised archive format: %s", filepath.Base(archivePath))
	}
}

func debugListZip(archivePath string) {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		log.Printf("debug: could not list zip: %v", err)
		return
	}
	defer r.Close()
	log.Printf("debug: zip contains %d entries:", len(r.File))
	for _, f := range r.File {
		log.Printf("debug:   [zip] %s (%d bytes)", f.Name, f.UncompressedSize64)
	}
}

func debugWalkDir(label, dir string) {
	log.Printf("debug: %s dir contents (%s):", label, dir)
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || p == dir {
			return nil
		}
		kind := "file"
		if info.IsDir() {
			kind = "dir "
		}
		rel, _ := filepath.Rel(dir, p)
		log.Printf("debug:   [%s] %s", kind, rel)
		return nil
	})
}

// unwrapSingleDir returns the sole subdirectory of dir when dir contains exactly
// one entry and that entry is a directory; otherwise it returns dir unchanged.
// This handles the common GitHub release layout where everything sits inside a
// single top-level folder (e.g. tool-v1.0.0-windows-amd64/).
func unwrapSingleDir(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		return dir
	}
	return filepath.Join(dir, entries[0].Name())
}

// copyDirContents recursively copies all files and subdirectories from src into dst.
func copyDirContents(src, dst string, debug bool) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if debug {
			log.Printf("debug: copy %s → %s", rel, target)
		}
		if err := copyFile(p, target); err != nil {
			log.Printf("debug: copy error: %v", err)
			return err
		}
		return nil
	})
}

// copyFile copies src to dst, creating parent directories as needed.
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("install: create dir: %w", err)
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("install: open for copy: %w", err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("install: create for copy: %w", err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("install: copy: %w", err)
	}
	return nil
}

// moveFile attempts an atomic rename; falls back to copy+delete across volumes.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	// Cross-volume fallback.
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("moveFile open src: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("moveFile create dst: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("moveFile copy: %w", err)
	}

	// Close before removing on Windows (file locks).
	out.Close()
	in.Close()
	return os.Remove(src)
}

func repoBaseName(repo string) string {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return repo
}

// resolveDestName returns the final .exe filename for the installed binary.
// If binaryName is provided it is used (with .exe appended if missing);
// otherwise the repo base name is used.
func resolveDestName(binaryName, repoName string) string {
	name := repoName
	if binaryName != "" {
		name = binaryName
	}
	if !strings.HasSuffix(strings.ToLower(name), ".exe") {
		name += ".exe"
	}
	return name
}
