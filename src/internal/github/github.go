package github

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"time"
)

const apiBase = "https://api.github.com"

// Release holds the fields we need from the GitHub Releases API response.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset represents a single downloadable file attached to a release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`   // bytes; 0 when the provider does not report it
	Digest             string `json:"digest"` // e.g. "sha256:<hex>"; empty when not reported
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

// LatestRelease fetches the latest release for the given "owner/repo" string.
// It uses the GITHUB_TOKEN environment variable as a Bearer token when present.
func LatestRelease(repo, userAgent string, debugHTTP bool) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", apiBase, repo)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	if debugHTTP {
		dump, _ := httputil.DumpRequestOut(req, true)
		log.Printf("debug-http: request:\n%s", dump)
	}

	resp, err := httpClient.Do(req)
	if debugHTTP && resp != nil {
		dump, _ := httputil.DumpResponse(resp, false)
		log.Printf("debug-http: response:\n%s", dump)
	}
	if err != nil {
		return nil, fmt.Errorf("github: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("github: repository %q not found or has no releases", repo)
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("github: API rate limit exceeded or bad token (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: unexpected status %d for %s", resp.StatusCode, repo)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("github: decode response: %w", err)
	}

	return &release, nil
}
