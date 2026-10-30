package github

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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
