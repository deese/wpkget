// Package herdr resolves the Herdr products, which publish a "latest.json"
// manifest on releases.herdr.dev instead of GitHub releases. The manifest lists
// the newest version with one download URL and SHA-256 per platform.
package herdr

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"sort"
	"strings"
	"time"

	"github.com/deese/wpkget/src/internal/github"
)

// Provider is the value of the provider field that selects this source.
const Provider = "herdr"

// Products lists the accepted repo names ("herdr/app", "herdr/core"). Anything
// else is rejected so the repo can never shape the manifest URL.
var Products = []string{"app", "core"}

// Both are variables so tests can point them at a local server.
var (
	manifestBase = "https://releases.herdr.dev"
	// downloadPrefix is the only location whose URLs are accepted from the manifest.
	downloadPrefix = "https://releases.herdr.dev/"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

// manifest is the subset of latest.json that is used.
type manifest struct {
	Version string                `json:"version"`
	Targets map[string]targetInfo `json:"targets"`
}

type targetInfo struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// LatestRelease describes the newest build of the product named by repo, given
// as "herdr/<product>" where <product> is "app" or "core"; the owner is
// ignored. The tag is the manifest version ("0.1.21") and there is one asset per
// platform, named after its URL and carrying the manifest SHA-256 as its digest.
func LatestRelease(repo, userAgent string, debugHTTP bool) (*github.Release, error) {
	product, err := productOf(repo)
	if err != nil {
		return nil, err
	}
	url := manifestBase + "/" + product + "/latest.json"

	body, err := get(url, userAgent, debugHTTP)
	if err != nil {
		return nil, fmt.Errorf("herdr: %q: %w", repo, err)
	}
	return parse(body, url)
}

// ReleaseByTag returns the latest release when its version is tag. The manifest
// only describes the newest build, so any other version is reported as not found.
func ReleaseByTag(repo, tag, userAgent string, debugHTTP bool) (*github.Release, error) {
	release, err := LatestRelease(repo, userAgent, debugHTTP)
	if err != nil {
		return nil, err
	}
	if release.TagName != tag {
		return nil, github.NewNotFound(fmt.Sprintf("herdr: version %q of %q is not available: only the latest (%s) is published", tag, repo, release.TagName))
	}
	return release, nil
}

// productOf extracts and validates the product from repo.
func productOf(repo string) (string, error) {
	product := repo[strings.LastIndex(repo, "/")+1:]
	for _, p := range Products {
		if product == p {
			return product, nil
		}
	}
	return "", fmt.Errorf("herdr: unknown product %q (use one of: %s, e.g. herdr/app)", product, strings.Join(Products, ", "))
}

// parse builds the release from a manifest. Targets whose URL is outside
// downloadPrefix or whose SHA-256 is missing are dropped, so a tampered
// manifest cannot send the download elsewhere or skip verification.
func parse(body []byte, url string) (*github.Release, error) {
	var m manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("herdr: parse %s: %w", url, err)
	}
	if m.Version == "" {
		return nil, fmt.Errorf("herdr: no version in %s", url)
	}

	keys := make([]string, 0, len(m.Targets))
	for k := range m.Targets {
		keys = append(keys, k)
	}
	sort.Strings(keys) // stable asset order

	var assets []github.Asset
	for _, k := range keys {
		t := m.Targets[k]
		if !strings.HasPrefix(t.URL, downloadPrefix) || t.SHA256 == "" {
			continue
		}
		assets = append(assets, github.Asset{
			Name:               t.URL[strings.LastIndex(t.URL, "/")+1:],
			BrowserDownloadURL: t.URL,
			Digest:             "sha256:" + strings.ToLower(t.SHA256),
		})
	}
	if len(assets) == 0 {
		return nil, fmt.Errorf("herdr: no usable download target in %s", url)
	}
	return &github.Release{TagName: m.Version, Assets: assets}, nil
}

// get returns the body of a 200 response, and a github.ErrNotFound-matching
// error for a 404.
func get(url, userAgent string, debugHTTP bool) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
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
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, github.NewNotFound(fmt.Sprintf("manifest not found (%s)", url))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}
