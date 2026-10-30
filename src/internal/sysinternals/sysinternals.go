// Package sysinternals resolves Sysinternals tools, which Microsoft publishes
// as zips on download.sysinternals.com instead of GitHub releases. The tool's
// Microsoft Learn page is the source of truth for its zip names, version and
// publication date.
package sysinternals

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"regexp"
	"strings"
	"time"

	"github.com/deese/wpkget/src/internal/github"
)

// Provider is the value of the provider field that selects this source.
const Provider = "sysinternals"

// Both are variables so tests can point them at a local server.
var (
	pageBase     = "https://learn.microsoft.com/en-us/sysinternals/downloads"
	downloadBase = "https://download.sysinternals.com/files"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

var (
	// zipLinkRe captures the zip name of each download link. Only the name is
	// used: the host is always downloadBase.
	zipLinkRe = regexp.MustCompile(`https://download\.sysinternals\.com/files/([A-Za-z0-9_.-]+\.zip)`)
	// versionRe captures the version from a heading such as "Autoruns v14.3".
	versionRe = regexp.MustCompile(`<h1[^>]*>[^<]*?\sv(\d[\d.]*)\s*</h1>`)
	// dateRe captures the publication date from the page metadata.
	dateRe = regexp.MustCompile(`<meta name="ms\.date" content="(\d{4}-\d{2}-\d{2})`)
)

// LatestRelease describes the current build of the tool named by repo, given as
// "sysinternals/<tool>" where <tool> is the last segment of the tool's Learn
// URL (e.g. "process-explorer", "pstools"); the owner is ignored and the case
// does not matter. Sysinternals publishes no release metadata, so the tag is
// "<version>-<date>" read from the page ("v14.3-2026-06-17"), or only the date
// for pages without a version, and the assets are the zips the page links to.
func LatestRelease(repo, userAgent string, debugHTTP bool) (*github.Release, error) {
	slug := strings.ToLower(repo[strings.LastIndex(repo, "/")+1:])
	pageURL := pageBase + "/" + slug

	body, err := get(http.MethodGet, pageURL, userAgent, debugHTTP)
	if err != nil {
		return nil, fmt.Errorf("sysinternals: %q: %w", repo, err)
	}
	page := string(body)

	assets := zipAssets(page, userAgent, debugHTTP)
	if len(assets) == 0 {
		return nil, fmt.Errorf("sysinternals: no download link found on %s", pageURL)
	}

	date := ""
	if m := dateRe.FindStringSubmatch(page); m != nil {
		date = m[1]
	}
	tag := date
	if m := versionRe.FindStringSubmatch(page); m != nil {
		tag = "v" + m[1]
		if date != "" {
			tag += "-" + date
		}
	}
	if tag == "" {
		return nil, fmt.Errorf("sysinternals: no version or date found on %s", pageURL)
	}
	return &github.Release{TagName: tag, Assets: assets}, nil
}

// ReleaseByTag returns the current build when tag is its version. Older builds
// are not published, so any other tag is not found.
func ReleaseByTag(repo, tag, userAgent string, debugHTTP bool) (*github.Release, error) {
	release, err := LatestRelease(repo, userAgent, debugHTTP)
	if err != nil {
		return nil, err
	}
	if release.TagName != tag {
		return nil, github.NewNotFound(fmt.Sprintf(
			"sysinternals: no build %q of %q (only the current build %s is available)", tag, repo, release.TagName))
	}
	return release, nil
}

// zipAssets lists the distinct zips linked from page, in page order. The size
// comes from a HEAD request and is left 0 when that fails.
func zipAssets(page, userAgent string, debugHTTP bool) []github.Asset {
	var assets []github.Asset
	seen := map[string]bool{}
	for _, m := range zipLinkRe.FindAllStringSubmatch(page, -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		asset := github.Asset{Name: name, BrowserDownloadURL: downloadBase + "/" + name}
		asset.Size = size(asset.BrowserDownloadURL, userAgent, debugHTTP)
		assets = append(assets, asset)
	}
	return assets
}

// size returns the Content-Length of url, or 0 when it cannot be determined.
func size(url, userAgent string, debugHTTP bool) int64 {
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return 0
	}
	resp, err := do(req, userAgent, debugHTTP)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength < 0 {
		return 0
	}
	return resp.ContentLength
}

// get returns the body of a 200 response, and a github.ErrNotFound-matching
// error for a 404.
func get(method, url, userAgent string, debugHTTP bool) ([]byte, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := do(req, userAgent, debugHTTP)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, github.NewNotFound(fmt.Sprintf("no such Sysinternals tool (%s not found; use the last segment of its Microsoft Learn URL, e.g. process-explorer)", url))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

func do(req *http.Request, userAgent string, debugHTTP bool) (*http.Response, error) {
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	if debugHTTP {
		dump, _ := httputil.DumpRequestOut(req, false)
		log.Printf("debug-http: request:\n%s", dump)
	}
	resp, err := httpClient.Do(req)
	if debugHTTP && resp != nil {
		dump, _ := httputil.DumpResponse(resp, false)
		log.Printf("debug-http: response:\n%s", dump)
	}
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	return resp, nil
}
