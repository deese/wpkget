package install

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deese/wpkget/src/internal/github"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "asset.zip")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestVerifyDownload(t *testing.T) {
	content := "hello release asset"
	sum := sha256.Sum256([]byte(content))
	digest := "sha256:" + hex.EncodeToString(sum[:])
	path := writeFile(t, content)

	cases := []struct {
		name    string
		asset   github.Asset
		wantErr string
	}{
		{"no metadata skips checks", github.Asset{Name: "a.zip"}, ""},
		{"size ok", github.Asset{Name: "a.zip", Size: int64(len(content))}, ""},
		{"size mismatch", github.Asset{Name: "a.zip", Size: 11452}, "size mismatch"},
		{"digest ok", github.Asset{Name: "a.zip", Digest: digest}, ""},
		{"digest uppercase ok", github.Asset{Name: "a.zip", Digest: strings.ToUpper(digest)}, ""},
		{"digest mismatch", github.Asset{Name: "a.zip", Digest: "sha256:" + strings.Repeat("0", 64)}, "sha256 mismatch"},
		{"unknown digest algorithm ignored", github.Asset{Name: "a.zip", Digest: "md5:abc"}, ""},
		{"size and digest ok", github.Asset{Name: "a.zip", Size: int64(len(content)), Digest: digest}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyDownload(path, &tc.asset)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

// shortTimeouts makes the download timeouts small for the duration of a test.
func shortTimeouts(t *testing.T, header, idle time.Duration) {
	t.Helper()
	oh, oi := downloadHeaderTimeout, downloadIdleTimeout
	downloadHeaderTimeout, downloadIdleTimeout = header, idle
	t.Cleanup(func() { downloadHeaderTimeout, downloadIdleTimeout = oh, oi })
}

func TestDownloadHeaderTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select { // never answer, like a proxy that holds the response
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	shortTimeouts(t, 200*time.Millisecond, time.Minute)

	_, err := downloadURL(srv.URL, t.TempDir(), "tool.zip", "", false, nil)
	if err == nil || !strings.Contains(err.Error(), "no response from") {
		t.Fatalf("error = %v, want a header-timeout error", err)
	}
}

func TestDownloadIdleTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000000")
		w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		select { // then stall
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	shortTimeouts(t, time.Minute, 200*time.Millisecond)

	_, err := downloadURL(srv.URL, t.TempDir(), "tool.zip", "", false, nil)
	if err == nil || !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("error = %v, want a stall error", err)
	}
}

func TestSlowHeadersWithinTimeoutSucceed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond) // proxy buffering the file
		w.Write([]byte("payload"))
	}))
	t.Cleanup(srv.Close)
	shortTimeouts(t, 5*time.Second, 5*time.Second)

	var out bytes.Buffer
	path, err := downloadURL(srv.URL, t.TempDir(), "tool.zip", "", false, legacyFeedback{&out})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "payload" {
		t.Fatalf("content = %q", got)
	}
	if !strings.Contains(out.String(), "downloading tool.zip from") {
		t.Fatalf("download must be announced, got %q", out.String())
	}
}
