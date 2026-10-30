package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"
)

// apiBase is a variable so tests can point it at a local server.
var apiBase = "https://api.github.com"

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

// ErrNotFound matches (errors.Is) the errors returned when a repository or
// release does not exist, for both GitHub and GitLab.
var ErrNotFound = errors.New("not found")

type notFoundError struct{ msg string }

func (e notFoundError) Error() string        { return e.msg }
func (e notFoundError) Is(target error) bool { return target == ErrNotFound }

// NewNotFound returns an error with the given message that matches ErrNotFound.
func NewNotFound(msg string) error { return notFoundError{msg} }

// LatestRelease fetches the latest release for the given "owner/repo" string.
// It uses the GITHUB_TOKEN environment variable as a Bearer token when present.
func LatestRelease(repo, userAgent string, debugHTTP bool) (*Release, error) {
	return fetchRelease(
		fmt.Sprintf("%s/repos/%s/releases/latest", apiBase, repo),
		fmt.Sprintf("github: repository %q not found or has no releases", repo),
		repo, userAgent, debugHTTP)
}

// ReleaseByTag fetches the release published for the given tag.
func ReleaseByTag(repo, tag, userAgent string, debugHTTP bool) (*Release, error) {
	return fetchRelease(
		fmt.Sprintf("%s/repos/%s/releases/tags/%s", apiBase, repo, url.PathEscape(tag)),
		fmt.Sprintf("github: no release with tag %q in %q", tag, repo),
		repo, userAgent, debugHTTP)
}

// fetchRelease GETs apiURL and decodes a single release. notFound is the
// message used for HTTP 404.
func fetchRelease(apiURL, notFound, repo, userAgent string, debugHTTP bool) (*Release, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
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
		return nil, NewNotFound(notFound)
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
