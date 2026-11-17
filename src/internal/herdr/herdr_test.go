package herdr

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/deese/wpkget/src/internal/asset"
	"github.com/deese/wpkget/src/internal/github"
)

const appManifest = `{
  "version": "0.1.16",
  "core_min": "0.1.11",
  "targets": {
    "windows-x86_64": {"url": "%BASE%/app/0.1.16/herdr-app-windows-x86_64.tar.gz", "sha256": "ABDC5400679CB730223A43B2A5216999A1EDAD3C2E80368F0AD3C43EA9895FE3"},
    "linux-x86_64":   {"url": "%BASE%/app/0.1.16/herdr-app-linux-x86_64.tar.gz",   "sha256": "282d2632196b238206cfe66bfb61c0faf299ca1c91843bd076521bbe36571216"},
    "macos-aarch64":  {"url": "https://evil.example.com/herdr-app-macos-aarch64.tar.gz", "sha256": "99000875ead9333a33f5cb60274ccd9cf9d3e47360f03613a467295105c71c04"},
    "linux-aarch64":  {"url": "%BASE%/app/0.1.16/herdr-app-linux-aarch64.tar.gz"}
  }
}`

const coreManifest = `{
  "version": "0.1.21",
  "generation": 1,
  "targets": {
    "windows-x86_64": {"url": "%BASE%/core/0.1.21/herdr-core-windows-x86_64.zip", "sha256": "a8984634c35b58ba82d0aec078d5a5e6004da01d5a25b3daf822ab5e31c7312b"}
  }
}`

// newServer serves the manifests and points the package at them.
func newServer(t *testing.T) {
	t.Helper()
	var srv *httptest.Server
	body := func(s string) string { return strings.ReplaceAll(s, "%BASE%", srv.URL) }
	pages := map[string]string{
		"/app/latest.json":  appManifest,
		"/core/latest.json": coreManifest,
	}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body(page)))
	}))
	t.Cleanup(srv.Close)

	oldBase, oldPrefix := manifestBase, downloadPrefix
	manifestBase, downloadPrefix = srv.URL, srv.URL+"/"
	t.Cleanup(func() { manifestBase, downloadPrefix = oldBase, oldPrefix })
}

func TestLatestReleaseApp(t *testing.T) {
	newServer(t)
	rel, err := LatestRelease("herdr/app", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "0.1.16" {
		t.Errorf("tag = %q, want 0.1.16", rel.TagName)
	}
	// The foreign-host target and the one without sha256 are dropped.
	if len(rel.Assets) != 2 {
		t.Fatalf("assets = %+v, want 2", rel.Assets)
	}
	// Sorted by target key: linux-x86_64, windows-x86_64.
	win := rel.Assets[1]
	if win.Name != "herdr-app-windows-x86_64.tar.gz" {
		t.Errorf("name = %q", win.Name)
	}
	if want := "sha256:abdc5400679cb730223a43b2a5216999a1edad3c2e80368f0ad3c43ea9895fe3"; win.Digest != want {
		t.Errorf("digest = %q, want lowercase %q", win.Digest, want)
	}
}

func TestLatestReleaseCore(t *testing.T) {
	newServer(t)
	rel, err := LatestRelease("herdr/core", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "0.1.21" || len(rel.Assets) != 1 || rel.Assets[0].Name != "herdr-core-windows-x86_64.zip" {
		t.Errorf("unexpected release: %+v", rel)
	}
}

func TestOwnerIsIgnored(t *testing.T) {
	newServer(t)
	if _, err := LatestRelease("whoever/core", "", false); err != nil {
		t.Fatal(err)
	}
}

func TestUnknownProductIsRejected(t *testing.T) {
	newServer(t)
	for _, repo := range []string{"herdr/other", "herdr/core/..", "herdr/core?x=1", "herdr/", "app/core/x"} {
		if _, err := LatestRelease(repo, "", false); err == nil {
			t.Errorf("%q: expected an error", repo)
		}
	}
}

func TestReleaseByTag(t *testing.T) {
	newServer(t)
	if rel, err := ReleaseByTag("herdr/core", "0.1.21", "", false); err != nil || rel.TagName != "0.1.21" {
		t.Fatalf("latest tag: rel=%v err=%v", rel, err)
	}
	_, err := ReleaseByTag("herdr/core", "0.1.0", "", false)
	if !errors.Is(err, github.ErrNotFound) {
		t.Errorf("old tag: err = %v, want ErrNotFound", err)
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"invalid json": `not json`,
		"no version":   `{"targets":{"a":{"url":"` + downloadPrefix + `x.zip","sha256":"aa"}}}`,
		"no targets":   `{"version":"1.0.0","targets":{}}`,
		"only foreign": `{"version":"1.0.0","targets":{"a":{"url":"https://evil.example.com/x.zip","sha256":"aa"}}}`,
	}
	for name, body := range cases {
		if _, err := parse([]byte(body), "test"); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestManifestNotFound(t *testing.T) {
	newServer(t)
	old := manifestBase
	manifestBase += "/missing"
	t.Cleanup(func() { manifestBase = old })
	_, err := LatestRelease("herdr/app", "", false)
	if !errors.Is(err, github.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// TestAssetSelection checks that the shared selector picks this platform's
// build from a herdr release (x86_64 naming, .tar.gz for app and .zip for core).
func TestAssetSelection(t *testing.T) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		t.Skip("expects windows/amd64")
	}
	newServer(t)
	for repo, want := range map[string]string{
		"herdr/app":  "herdr-app-windows-x86_64.tar.gz",
		"herdr/core": "herdr-core-windows-x86_64.zip",
	} {
		rel, err := LatestRelease(repo, "", false)
		if err != nil {
			t.Fatal(err)
		}
		chosen, err := asset.Select(rel.Assets, repo[strings.LastIndex(repo, "/")+1:], "", false, false)
		if err != nil {
			t.Fatalf("%s: %v", repo, err)
		}
		if chosen.Name != want {
			t.Errorf("%s: chose %q, want %q", repo, chosen.Name, want)
		}
	}
}
