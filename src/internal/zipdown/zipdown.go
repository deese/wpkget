package zipdown

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// ErrNotConfigured is returned when zipdown_url is not set in config.
// The install pipeline uses this sentinel to fall back to direct download.
var ErrNotConfigured = errors.New("zipdown is not configured")

// Client wraps the zipdown service.
type Client struct {
	url   string
	token string
	http  *http.Client
}

// New creates a Client. Both url and token may be empty strings; in that case
// every call to Wrap returns ErrNotConfigured.
func New(url, token string) *Client {
	return &Client{
		url:   url,
		token: token,
		http:  &http.Client{Timeout: 120 * time.Second},
	}
}

// Wrap asks the zipdown service to download assetURL and return it wrapped in
// a zip file. The resulting zip is saved to destDir and its full path is
// returned.
//
// Returns ErrNotConfigured when the client was created without a service URL.
func (c *Client) Wrap(assetURL, destDir string) (string, error) {
	if c.url == "" {
		return "", ErrNotConfigured
	}

	body, err := json.Marshal(map[string]string{"url": assetURL})
	if err != nil {
		return "", fmt.Errorf("zipdown: marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.url+"/wrap", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("zipdown: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("zipdown: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("zipdown: unexpected status %d", resp.StatusCode)
	}

	destPath := filepath.Join(destDir, "wrapped.zip")
	f, err := os.Create(destPath)
	if err != nil {
		return "", fmt.Errorf("zipdown: create output file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("zipdown: write response: %w", err)
	}

	return destPath, nil
}
