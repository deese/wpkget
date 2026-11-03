package github

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deese/wpkget/src/internal/config"
)

const testFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <id>tag:github.com,2008:https://github.com/o/r/releases</id>
  <entry>
    <id>tag:github.com,2008:Repository/1/v1.2.3</id>
    <link rel="alternate" type="text/html" href="https://github.com/o/r/releases/tag/v1.2.3"/>
  </entry>
  <entry>
    <link rel="alternate" type="text/html" href="https://github.com/o/r/releases/tag/v1.2.2"/>
  </entry>
</feed>`

const testAssetsPage = `<ul>
<li><a href="/o/r/releases/download/v1.2.3/r_1.2.3_windows_amd64.zip" rel="nofollow">r_1.2.3_windows_amd64.zip</a></li>
<li><a href="/o/r/releases/download/v1.2.3/my%20tool.zip">my tool.zip</a></li>
<li><a href="/o/r/releases/download/v1.2.3/r_1.2.3_windows_amd64.zip">dup</a></li>
<li><a href="/o/r/archive/refs/tags/v1.2.3.zip">Source code (zip)</a></li>
</ul>`

func newRemoteServer(t *testing.T) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/o/r/releases.atom", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(testFeed))
	})
	mux.HandleFunc("/o/r/releases/expanded_assets/v1.2.3", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(testAssetsPage))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	Configure(&config.Config{GitHubRemote: srv.URL + "/"}) // trailing slash must be tolerated
	t.Cleanup(func() { Configure(&config.Config{}) })
}

func TestRemoteLatestRelease(t *testing.T) {
	newRemoteServer(t)

	rel, err := LatestRelease("o/r", "ua", false)
	if err != nil {
		t.Fatalf("LatestRelease: %v", err)
	}
	if rel.TagName != "v1.2.3" {
		t.Errorf("tag = %q, want v1.2.3", rel.TagName)
	}
	if len(rel.Assets) != 2 {
		t.Fatalf("assets = %d, want 2 (deduplicated, no source archive): %+v", len(rel.Assets), rel.Assets)
	}
	if rel.Assets[1].Name != "my tool.zip" {
		t.Errorf("asset name = %q, want decoded %q", rel.Assets[1].Name, "my tool.zip")
	}
	want := remoteBase + "/o/r/releases/download/v1.2.3/my%20tool.zip"
	if rel.Assets[1].BrowserDownloadURL != want {
		t.Errorf("download url = %q, want %q", rel.Assets[1].BrowserDownloadURL, want)
	}
	if rel.Assets[0].Size != 0 || rel.Assets[0].Digest != "" {
		t.Errorf("remote assets must not claim size or digest: %+v", rel.Assets[0])
	}
}

func TestRemoteLatestSkipsReleasesWithoutAssets(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/o/r/releases.atom", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`<feed xmlns="http://www.w3.org/2005/Atom">
<entry><link href="https://github.com/o/r/releases/tag/helper-0.4.34"/></entry>
<entry><link href="https://github.com/o/r/releases/tag/v1.2.3"/></entry></feed>`))
	})
	mux.HandleFunc("/o/r/releases/expanded_assets/helper-0.4.34", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`<ul></ul>`))
	})
	mux.HandleFunc("/o/r/releases/expanded_assets/v1.2.3", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(testAssetsPage))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	Configure(&config.Config{GitHubRemote: srv.URL})
	t.Cleanup(func() { Configure(&config.Config{}) })

	rel, err := LatestRelease("o/r", "ua", false)
	if err != nil {
		t.Fatalf("LatestRelease: %v", err)
	}
	if rel.TagName != "v1.2.3" {
		t.Errorf("tag = %q, want v1.2.3 (the assetless helper release must be skipped)", rel.TagName)
	}
}

func TestRemoteReleaseByTagNotFound(t *testing.T) {
	newRemoteServer(t)

	_, err := ReleaseByTag("o/r", "v9.9.9", "ua", false)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
