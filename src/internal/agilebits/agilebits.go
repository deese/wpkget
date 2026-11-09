// Package agilebits resolves AgileBits (1Password) products, which publish
// their builds as zips on cache.agilebits.com instead of GitHub releases. The
// product history page on app-updates.agilebits.com is the source of truth for
// the versions, their publication date and their download links.
package agilebits

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/deese/wpkget/src/internal/github"
)

// Provider is the value of the provider field that selects this source.
const Provider = "agilebits"

// Both are variables so tests can point them at a local server.
var (
	historyBase = "https://app-updates.agilebits.com/product_history"
	// downloadPrefix is the only host whose links are accepted from the page.
	downloadPrefix = "https://cache.agilebits.com/dist/"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

var (
	// articleRe starts each version block of the page; the attributes tell betas apart.
	articleRe = regexp.MustCompile(`<article\b([^>]*)>`)
	// versionRe captures the version at the start of the block's <h3>.
	versionRe = regexp.MustCompile(`(?s)<h3>\s*([^\s<]+)`)
	// zipLinkRe captures the URL of each zip link; the scheme and host are enforced by downloadPrefix.
	zipLinkRe = regexp.MustCompile(`href="(https?://[^"]+\.zip)"`)
)

// entry is one version listed on the history page.
type entry struct {
	version string
	beta    bool
	zips    []string
}

// LatestRelease describes the newest stable build of the product named by repo,
// given as "agilebits/<product>" where <product> is the product code of the
// history URL (e.g. "agilebits/CLI2" for the 1Password CLI); the owner is
// ignored. Betas are never returned. The tag is the version ("2.42.0") and the
// assets are the zips the page links to.
func LatestRelease(repo, userAgent string, debugHTTP bool) (*github.Release, error) {
	entries, pageURL, err := history(repo, userAgent, debugHTTP)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.beta {
			return release(e, userAgent, debugHTTP), nil
		}
	}
	return nil, fmt.Errorf("agilebits: no stable release found in %s", pageURL)
}

// ReleaseByTag returns the build whose version is tag, beta or not.
func ReleaseByTag(repo, tag, userAgent string, debugHTTP bool) (*github.Release, error) {
	entries, _, err := history(repo, userAgent, debugHTTP)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.version == tag {
			return release(e, userAgent, debugHTTP), nil
		}
	}
	return nil, github.NewNotFound(fmt.Sprintf("agilebits: no version %q of %q in its history", tag, repo))
}

// history fetches and parses the history page of the product named by repo,
// newest first as the page lists it. Entries without a zip are dropped.
func history(repo, userAgent string, debugHTTP bool) ([]entry, string, error) {
	product := repo[strings.LastIndex(repo, "/")+1:]
	pageURL := historyBase + "/" + strings.ToUpper(product)

	body, err := get(pageURL, userAgent, debugHTTP)
	if err != nil {
		return nil, pageURL, fmt.Errorf("agilebits: %q: %w", repo, err)
	}
	entries := parse(string(body))
	if len(entries) == 0 {
		return nil, pageURL, fmt.Errorf("agilebits: no download link found in %s", pageURL)
	}
	return entries, pageURL, nil
}

// parse splits the page into its version blocks. Only links under
// downloadPrefix are kept, so a link to another host is never followed.
func parse(page string) []entry {
	idx := articleRe.FindAllStringSubmatchIndex(page, -1)
	var entries []entry
	for i, loc := range idx {
		end := len(page)
		if i+1 < len(idx) {
			end = idx[i+1][0]
		}
		block := page[loc[1]:end]

		v := versionRe.FindStringSubmatch(block)
		if v == nil {
			continue
		}
		e := entry{version: v[1], beta: strings.Contains(page[loc[2]:loc[3]], "beta")}
		seen := map[string]bool{}
		for _, m := range zipLinkRe.FindAllStringSubmatch(block, -1) {
			if strings.HasPrefix(m[1], downloadPrefix) && !seen[m[1]] {
				seen[m[1]] = true
				e.zips = append(e.zips, m[1])
			}
		}
		if len(e.zips) > 0 {
			entries = append(entries, e)
		}
	}
	return entries
}

// release builds the release of e. The size of each zip comes from a HEAD
// request, run concurrently, and is left 0 when that fails.
func release(e entry, userAgent string, debugHTTP bool) *github.Release {
	assets := make([]github.Asset, len(e.zips))
	var wg sync.WaitGroup
	for i, url := range e.zips {
		assets[i] = github.Asset{Name: url[strings.LastIndex(url, "/")+1:], BrowserDownloadURL: url}
		wg.Add(1)
		go func() {
			defer wg.Done()
			assets[i].Size = size(url, userAgent, debugHTTP)
		}()
	}
	wg.Wait()
	return &github.Release{TagName: e.version, Prerelease: e.beta, Assets: assets}
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
func get(url, userAgent string, debugHTTP bool) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := do(req, userAgent, debugHTTP)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, github.NewNotFound(fmt.Sprintf("no such AgileBits product (%s not found; use its product code, e.g. CLI2)", url))
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
