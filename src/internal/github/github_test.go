package github

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/deese/wpkget/src/internal/config"
)

func TestReleaseEndpoints(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		if gotPath == "/repos/o/r/releases/tags/missing" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"tag_name":"v1.2.3","assets":[{"name":"a.zip","browser_download_url":"u","size":5,"digest":"sha256:ab"}]}`))
	}))
	t.Cleanup(srv.Close)
	old := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = old })

	rel, err := LatestRelease("o/r", "", false)
	if err != nil || gotPath != "/repos/o/r/releases/latest" || rel.TagName != "v1.2.3" {
		t.Fatalf("latest: err=%v path=%q rel=%+v", err, gotPath, rel)
	}
	if a := rel.Assets[0]; a.Size != 5 || a.Digest != "sha256:ab" {
		t.Errorf("size/digest not decoded: %+v", a)
	}

	rel, err = ReleaseByTag("o/r", "v1.2.3", "", false)
	if err != nil || gotPath != "/repos/o/r/releases/tags/v1.2.3" || rel.TagName != "v1.2.3" {
		t.Fatalf("by tag: err=%v path=%q", err, gotPath)
	}

	if _, err := ReleaseByTag("o/r", "missing", "", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a missing tag must match ErrNotFound, got %v", err)
	}
}

// authHeaderServer points apiBase at a server that records the Authorization header.
func authHeaderServer(t *testing.T) *string {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.Write([]byte(`{"tag_name":"v1","assets":[]}`))
	}))
	t.Cleanup(srv.Close)
	old := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = old; Configure(&config.Config{}) })
	return &got
}

func TestTokenFromConfiguredEnv(t *testing.T) {
	got := authHeaderServer(t)
	t.Setenv("TEST_GH_TOKEN", "secret")
	Configure(&config.Config{GitHub: config.GitHubConfig{TokenEnv: "TEST_GH_TOKEN"}})

	if _, err := LatestRelease("o/r", "", false); err != nil {
		t.Fatal(err)
	}
	if *got != "Bearer secret" {
		t.Errorf("Authorization = %q", *got)
	}
}

func TestTokenDefaultsToGitHubTokenEnv(t *testing.T) {
	got := authHeaderServer(t)
	t.Setenv("GITHUB_TOKEN", "from-default-env")
	Configure(&config.Config{})

	if _, err := LatestRelease("o/r", "", false); err != nil {
		t.Fatal(err)
	}
	if *got != "Bearer from-default-env" {
		t.Errorf("Authorization = %q", *got)
	}
}

func TestTokenFromPassWhenEnvUnset(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no POSIX shell available")
	}
	script := filepath.Join(t.TempDir(), "fakepass")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'tok-from-pass\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := authHeaderServer(t)
	t.Setenv("GITHUB_TOKEN", "")
	Configure(&config.Config{PassCmd: script, GitHub: config.GitHubConfig{TokenPass: "github/wpkget"}})

	if _, err := LatestRelease("o/r", "", false); err != nil {
		t.Fatal(err)
	}
	if *got != "Bearer tok-from-pass" {
		t.Errorf("Authorization = %q", *got)
	}
}

func TestNoTokenSendsNoAuthorization(t *testing.T) {
	got := authHeaderServer(t)
	t.Setenv("GITHUB_TOKEN", "")
	Configure(&config.Config{})

	if _, err := LatestRelease("o/r", "", false); err != nil {
		t.Fatal(err)
	}
	if *got != "" {
		t.Errorf("Authorization = %q, want none", *got)
	}
}

func TestPassFailureIsReported(t *testing.T) {
	authHeaderServer(t)
	t.Setenv("GITHUB_TOKEN", "")
	Configure(&config.Config{PassCmd: "wpkget-no-such-pass-binary", GitHub: config.GitHubConfig{TokenPass: "x"}})

	if _, err := LatestRelease("o/r", "", false); err == nil {
		t.Fatal("a failing pass lookup must not fall back to anonymous requests silently")
	}
}
