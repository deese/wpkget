package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/deese/wpkget/src/internal/asset"
	"github.com/deese/wpkget/src/internal/github"
	"github.com/deese/wpkget/src/internal/gitlab"
	"github.com/deese/wpkget/src/internal/zipdown"
)

// Options controls a single install run.
type Options struct {
	Repo       string
	Provider   string // "github" (default) or "gitlab"
	Host       string // GitLab host (empty = gitlab.com)
	BinDir     string
	BinaryName string // optional: rename the installed .exe to this name (without extension)
	Tag        string // Fetch: release tag to download; empty means the latest release
	Match      string // optional: glob pattern to select the release asset (e.g. "*windows-amd64*.zip")
	All        bool   // when true, copy all archive contents to BinDir instead of extracting a single .exe
	NoExtract  bool   // Fetch: keep the downloaded file as is instead of extracting it
	Force      bool   // Fetch: overwrite an existing destination
	DryRun     bool
	Verbose    bool
	Debug      bool            // enable step-by-step diagnostic output
	DebugHTTP  bool            // dump outgoing HTTP requests and response bodies
	UserAgent  string          // custom User-Agent header for HTTP requests
	Progress   io.Writer       // where to draw the download progress bar; nil disables it
	Feedback   Feedback        // download events for the modern output; takes precedence over Progress
	Zipdown    *zipdown.Client // nil means zipdown is disabled
}

// Result is returned by a successful Run.
type Result struct {
	Version    string
	BinaryPath string        // final location of the installed binary
	Size       int64         // bytes downloaded
	Duration   time.Duration // time spent downloading
}

// httpClient has no overall timeout: downloads are bounded by the header and idle timeouts in downloadURL.
var httpClient = &http.Client{}

// latestRelease dispatches to the correct provider based on opts.Provider.
func latestRelease(provider, host, repo, userAgent string, debugHTTP bool) (*github.Release, error) {
	if provider == "gitlab" {
		return gitlab.LatestRelease(host, repo, userAgent, debugHTTP)
	}
	return github.LatestRelease(repo, userAgent, debugHTTP)
}

// dprint logs a debug message when enabled is true.
func dprint(enabled bool, format string, args ...any) {
	if enabled {
		log.Printf(format, args...)
	}
}

// Run executes the full install pipeline for a single repository:
// resolve → download → decompress → move → cleanup.
func Run(opts Options) (*Result, error) {
	release, err := latestRelease(opts.Provider, opts.Host, opts.Repo, opts.UserAgent, opts.DebugHTTP)
	if err != nil {
		return nil, err
	}

	chosen, err := pickAsset(opts, release)
	if err != nil {
		return nil, err
	}

	repoName := repoBaseName(opts.Repo)
	destName := resolveDestName(opts.BinaryName, repoName)

	if opts.DryRun {
		if opts.Feedback == nil { // the modern output reports the dry run itself
			fmt.Printf("dry-run: would install %s %s\n", opts.Repo, release.TagName)
			fmt.Printf("dry-run: asset      %s\n", chosen.Name)
			fmt.Printf("dry-run: url        %s\n", chosen.BrowserDownloadURL)
			if opts.All {
				fmt.Printf("dry-run: dest (all) %s\n", opts.BinDir)
			} else {
				fmt.Printf("dry-run: dest       %s\n", filepath.Join(opts.BinDir, destName))
			}
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
	archivePath, dlSize, dlDur, err := downloadTimed(opts, chosen, tmpDir)
	if err != nil {
		return nil, err
	}

	// Ensure the destination directory exists.
	if err := os.MkdirAll(opts.BinDir, 0o755); err != nil {
		return nil, fmt.Errorf("install: create bin dir: %w", err)
	}

	if opts.All {
		// Copy all archive contents to BinDir.
		if err := extractAndCopyAll(archivePath, opts.BinDir, tmpDir, opts.Debug, opts.DebugHTTP); err != nil {
			return nil, err
		}
		if opts.Verbose {
			log.Printf("installed all files from %s %s → %s", opts.Repo, release.TagName, opts.BinDir)
		}
		return &Result{Version: release.TagName, BinaryPath: opts.BinDir, Size: dlSize, Duration: dlDur}, nil
	}

	// Locate the single binary inside the archive (or use directly for bare .exe).
	binaryInTmp, err := extractBinary(archivePath, tmpDir, repoName, opts.DebugHTTP)
	if err != nil {
		return nil, err
	}

	dest := filepath.Join(opts.BinDir, destName)
	if err := moveFile(binaryInTmp, dest); err != nil {
		return nil, fmt.Errorf("install: move binary: %w", err)
	}

	if runtime.GOOS != "windows" {
		if err := os.Chmod(dest, 0o755); err != nil {
			return nil, fmt.Errorf("install: chmod binary: %w", err)
		}
	}

	return &Result{Version: release.TagName, BinaryPath: dest, Size: dlSize, Duration: dlDur}, nil
}

// sourceLabel describes where a release comes from, e.g. "github" or "gitlab git.example.com".
func sourceLabel(provider, host string) string {
	if provider == "" {
		provider = "github"
	}
	if host != "" {
		return provider + " " + host
	}
	return provider
}

// ResolveURL returns the download URL for the latest release asset
// without downloading anything.
func ResolveURL(provider, host, repo, match, userAgent string, verbose, debugHTTP bool) (string, string, error) {
	release, err := latestRelease(provider, host, repo, userAgent, debugHTTP)
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
		dprint(opts.Debug, "debug: zipdown wrap %s", chosen.BrowserDownloadURL)
		path, err := opts.Zipdown.Wrap(chosen.BrowserDownloadURL, tmpDir, opts.DebugHTTP)
		if err == nil {
			return path, nil
		}
		if !errors.Is(err, zipdown.ErrNotConfigured) {
			return "", fmt.Errorf("install: zipdown: %w", err)
		}
		// Fall through to direct download.
	}

	dprint(opts.Debug, "debug: download %s → %s", chosen.BrowserDownloadURL, filepath.Join(tmpDir, chosen.Name))
	fb := opts.Feedback
	if fb == nil && opts.Progress != nil {
		fb = legacyFeedback{opts.Progress}
	}
	path, err := downloadURL(chosen.BrowserDownloadURL, tmpDir, chosen.Name, opts.UserAgent, opts.DebugHTTP, fb)
	if err != nil {
		return "", err
	}
	if err := verifyDownload(path, chosen); err != nil {
		return "", err
	}
	dprint(opts.Debug, "debug: verified %s (size %d, digest %q)", chosen.Name, chosen.Size, chosen.Digest)
	return path, nil
}

// downloadURL streams a URL to destDir/<name> and returns the full path.
// When fb is non-nil it is told about the wait for the server and the transfer
// progress, so the caller can show it.
func downloadURL(url, destDir, name, userAgent string, debugHTTP bool, fb Feedback) (string, error) {
	ctx, cancel, timer := newDownloadContext()
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("install: build request: %w", err)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}

	if debugHTTP {
		dump, _ := httputil.DumpRequestOut(req, true)
		log.Printf("debug-http: request:\n%s", dump)
	}

	// GitLab-hosted assets need the per-host token and TLS settings.
	client := httpClient
	glClient, err := gitlab.Prepare(req)
	if err != nil {
		return "", fmt.Errorf("install: %w", err)
	}
	if glClient != nil {
		c := *glClient
		c.Timeout = 0 // bounded by the header and idle timeouts instead
		client = &c
	}

	stopWait := func() {}
	if fb != nil {
		stopWait = fb.Waiting(name, req.URL.Host)
	}
	resp, err := client.Do(req)
	stopWait()
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("install: no response from %s after %s", req.URL.Host, downloadHeaderTimeout)
		}
		return "", fmt.Errorf("install: download %s: %w", url, err)
	}
	defer resp.Body.Close()
	timer.Reset(downloadIdleTimeout)

	if debugHTTP {
		dump, _ := httputil.DumpResponse(resp, false)
		log.Printf("debug-http: response:\n%s", dump)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("install: download returned status %d", resp.StatusCode)
	}

	dest := filepath.Join(destDir, name)
	f, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("install: create file: %w", err)
	}
	defer f.Close()

	var body io.Reader = &idleReader{r: resp.Body, timer: timer}
	finished := false
	if fb != nil {
		var finish func(ok bool)
		body, finish = fb.Wrap(body, name, resp.ContentLength)
		defer func() { finish(finished) }()
	}

	if _, err := io.Copy(f, body); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("install: download stalled: no data received for %s", downloadIdleTimeout)
		}
		return "", fmt.Errorf("install: write download: %w", err)
	}
	finished = true

	return dest, nil
}

// extractBinary decompresses archivePath into a subdirectory of tmpDir and
// returns the path to the selected binary.
// If archivePath is itself a bare binary it is returned as-is.
func extractBinary(archivePath, tmpDir, repoName string, debugHTTP bool) (string, error) {
	lower := strings.ToLower(archivePath)

	switch {
	case strings.HasSuffix(lower, ".exe"):
		return archivePath, nil

	case strings.HasSuffix(lower, ".zip"):
		extractDir := filepath.Join(tmpDir, "extracted")
		if err := extractZip(archivePath, extractDir, debugHTTP); err != nil {
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
		outPath := strings.TrimSuffix(archivePath, ".gz")
		if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(outPath), ".exe") {
			outPath += ".exe"
		}
		if err := extractGz(archivePath, outPath); err != nil {
			return "", err
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(outPath, 0o755); err != nil {
				return "", fmt.Errorf("install: chmod extracted binary: %w", err)
			}
		}
		return outPath, nil

	default:
		// Bare extensionless file on non-Windows: treat as a direct binary.
		if runtime.GOOS != "windows" && filepath.Ext(archivePath) == "" {
			if err := os.Chmod(archivePath, 0o755); err != nil {
				return "", fmt.Errorf("install: chmod bare binary: %w", err)
			}
			return archivePath, nil
		}
		return "", fmt.Errorf("install: unrecognised archive format: %s", filepath.Base(archivePath))
	}
}

// findBinary walks dir and returns the path of the best-matching binary.
// On Windows it looks for .exe files. On other platforms it looks for files
// with the executable bit set and no extension, falling back to .exe if none found.
func findBinary(dir, repoName string) (string, error) {
	var candidates []string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if isBinaryCandidate(p, info) {
			candidates = append(candidates, p)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("install: walk extracted dir: %w", err)
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("install: no executable binary found in archive")
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}

	// Prefer the one whose base name contains the repo name.
	for _, p := range candidates {
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(p), ".exe"))
		if strings.Contains(base, strings.ToLower(repoName)) {
			return p, nil
		}
	}

	log.Printf("warning: multiple binaries found, using %s", filepath.Base(candidates[0]))
	return candidates[0], nil
}

// isBinaryCandidate reports whether a file should be considered as the target binary.
func isBinaryCandidate(p string, info os.FileInfo) bool {
	lower := strings.ToLower(p)
	if strings.HasSuffix(lower, ".exe") {
		return true
	}
	if runtime.GOOS != "windows" && filepath.Ext(p) == "" && (info.Mode()&0o111 != 0) {
		return true
	}
	return false
}

// titleRegex extracts the content of an HTML <title> tag.
var titleRegex = regexp.MustCompile(`(?i)<title[^>]*>([^<]*)</title>`)

// extractZip unpacks a .zip archive into destDir.
// If the file is not a valid zip it inspects the contents: when it looks like
// HTML the <title> is logged; when dumpBody is true the full body is dumped.
func extractZip(src, destDir string, dumpBody bool) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		inspectInvalidZip(src, dumpBody)
		return fmt.Errorf("install: open zip %s: %w", src, err)
	}
	defer r.Close()

	for _, f := range r.File {
		if err := extractZipEntry(f, destDir); err != nil {
			return err
		}
	}
	return nil
}

// inspectInvalidZip reads the first 256 KiB of src and, when it appears to be
// HTML, prints the <title> tag. If dumpBody is true the whole file is dumped.
func inspectInvalidZip(src string, dumpBody bool) {
	f, err := os.Open(src)
	if err != nil {
		return
	}
	defer f.Close()

	// Read enough to reliably find an HTML title (256 KiB cap).
	limit := 256 * 1024
	buf := make([]byte, limit)
	n, _ := f.Read(buf)
	if n == 0 {
		return
	}
	snippet := string(buf[:n])

	// Look for HTML indicators.
	trimmed := strings.TrimLeft(snippet, " \t\r\n")
	isHTML := strings.HasPrefix(strings.ToLower(trimmed), "<!doctype html") ||
		strings.HasPrefix(strings.ToLower(trimmed), "<html") ||
		strings.Contains(strings.ToLower(snippet), "<head")

	if isHTML {
		if m := titleRegex.FindStringSubmatch(snippet); len(m) > 1 {
			log.Printf("warning: downloaded file is HTML, title: %q", strings.TrimSpace(m[1]))
		} else {
			log.Printf("warning: downloaded file is HTML (no <title> found)")
		}
	}

	if dumpBody {
		// Read the remainder and dump everything.
		var rest bytes.Buffer
		io.Copy(&rest, f)
		full := append(buf[:n], rest.Bytes()...)
		log.Printf("debug-http: invalid zip body (%d bytes):\n%s", len(full), full)
	}
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
		return fmt.Errorf("install: open tar.gz %s: %w", src, err)
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
	cleanDest := filepath.Clean(destDir)
	if target != cleanDest && !strings.HasPrefix(target, cleanDest+string(os.PathSeparator)) {
		return fmt.Errorf("install: tar-slip detected in entry %q", hdr.Name)
	}

	switch hdr.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(target, 0o755)

	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("install: create dir for tar entry: %w", err)
		}
		perm := hdr.FileInfo().Mode().Perm()
		if perm == 0 {
			perm = 0o644
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
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
		return fmt.Errorf("install: open gz %s: %w", src, err)
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
func extractAndCopyAll(archivePath, destDir, tmpDir string, debug, debugHTTP bool) error {
	lower := strings.ToLower(archivePath)

	switch {
	case strings.HasSuffix(lower, ".exe"):
		dst := filepath.Join(destDir, filepath.Base(archivePath))
		dprint(debug, "debug: bare exe → %s", dst)
		return copyFile(archivePath, dst)

	case strings.HasSuffix(lower, ".zip"):
		dprint(debug, "debug: zip contains entries:")
		if debug {
			logZipEntries(archivePath)
		}
		extractDir := filepath.Join(tmpDir, "extracted")
		if err := extractZip(archivePath, extractDir, debugHTTP); err != nil {
			return err
		}
		dprint(debug, "debug: extracted dir contents:")
		if debug {
			logDirContents(extractDir)
		}
		src := unwrapSingleDir(extractDir)
		if src != extractDir {
			dprint(debug, "debug: unwrapped single top-level dir → %s", filepath.Base(src))
		} else {
			dprint(debug, "debug: no single top-level dir to unwrap, copying from %s", extractDir)
		}
		return copyDirContents(src, destDir, debug)

	case strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz"):
		extractDir := filepath.Join(tmpDir, "extracted")
		if err := extractTarGz(archivePath, extractDir); err != nil {
			return err
		}
		dprint(debug, "debug: extracted dir contents:")
		if debug {
			logDirContents(extractDir)
		}
		src := unwrapSingleDir(extractDir)
		if src != extractDir {
			dprint(debug, "debug: unwrapped single top-level dir → %s", filepath.Base(src))
		} else {
			dprint(debug, "debug: no single top-level dir to unwrap, copying from %s", extractDir)
		}
		return copyDirContents(src, destDir, debug)

	case strings.HasSuffix(lower, ".gz"):
		outPath := strings.TrimSuffix(archivePath, ".gz")
		if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(outPath), ".exe") {
			outPath += ".exe"
		}
		if err := extractGz(archivePath, outPath); err != nil {
			return err
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(outPath, 0o755); err != nil {
				return fmt.Errorf("install: chmod extracted binary: %w", err)
			}
		}
		dst := filepath.Join(destDir, filepath.Base(outPath))
		dprint(debug, "debug: gz → %s", dst)
		return copyFile(outPath, dst)

	default:
		return fmt.Errorf("install: unrecognised archive format: %s", filepath.Base(archivePath))
	}
}

func logZipEntries(archivePath string) {
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

func logDirContents(dir string) {
	log.Printf("debug: dir contents (%s):", dir)
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
		dprint(debug, "debug: copy %s → %s", rel, target)
		if err := copyFile(p, target); err != nil {
			dprint(debug, "debug: copy error: %v", err)
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
	// Last segment: GitLab projects may live in nested subgroups.
	return repo[strings.LastIndex(repo, "/")+1:]
}

// resolveDestName returns the final filename for the installed binary.
// On Windows it appends .exe if missing. On other platforms no extension is added.
func resolveDestName(binaryName, repoName string) string {
	name := repoName
	if binaryName != "" {
		name = binaryName
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		name += ".exe"
	}
	return name
}
