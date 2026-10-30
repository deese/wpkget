package sysinternals

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deese/wpkget/src/internal/github"
)

const explorerPage = `<meta name="ms.date" content="2026-10-15T00:00:00Z" />
<div class="content"><h1 id="process-explorer-v172">Process Explorer v17.2</h1></div>
<p>Published: October 15, 2026</p>
<p><a href="https://download.sysinternals.com/files/ProcessExplorer.zip">Download</a>
<a href="https://download.sysinternals.com/files/ProcessExplorer.zip">again</a></p>`

const suitePage = `<meta name="ms.date" content="2026-09-01T00:00:00Z" />
<h1 id="sysinternals-suite">Sysinternals Suite</h1>
<a href="https://download.sysinternals.com/files/SysinternalsSuite.zip">x64</a>
<a href="https://download.sysinternals.com/files/SysinternalsSuite-ARM64.zip">arm64</a>`

func newServer(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/pages/process-explorer":
			w.Write([]byte(explorerPage))
		case r.Method == http.MethodGet && r.URL.Path == "/pages/sysinternals-suite":
			w.Write([]byte(suitePage))
		case r.Method == http.MethodGet && r.URL.Path == "/pages/empty":
			w.Write([]byte("<h1>Nothing here v1.0</h1>"))
		case r.Method == http.MethodHead && r.URL.Path == "/files/ProcessExplorer.zip":
			w.Header().Set("Content-Length", "3813190")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	oldPage, oldDownload := pageBase, downloadBase
	pageBase, downloadBase = srv.URL+"/pages", srv.URL+"/files"
	t.Cleanup(func() { pageBase, downloadBase = oldPage, oldDownload })
}

func TestLatestRelease(t *testing.T) {
	newServer(t)

	rel, err := LatestRelease("sysinternals/Process-Explorer", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "v17.2-2026-10-15" {
		t.Errorf("tag = %q, want v17.2-2026-10-15", rel.TagName)
	}
	if len(rel.Assets) != 1 {
		t.Fatalf("assets = %d, want 1 (duplicate links collapse)", len(rel.Assets))
	}
	a := rel.Assets[0]
	if a.Name != "ProcessExplorer.zip" || a.BrowserDownloadURL != downloadBase+"/ProcessExplorer.zip" || a.Size != 3813190 {
		t.Errorf("unexpected asset: %+v", a)
	}
}

func TestLatestReleaseWithoutVersion(t *testing.T) {
	newServer(t)

	rel, err := LatestRelease("sysinternals/sysinternals-suite", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "2026-09-01" {
		t.Errorf("tag = %q, want the date alone", rel.TagName)
	}
	if len(rel.Assets) != 2 || rel.Assets[0].Name != "SysinternalsSuite.zip" || rel.Assets[1].Name != "SysinternalsSuite-ARM64.zip" {
		t.Errorf("assets must keep page order: %+v", rel.Assets)
	}
	if rel.Assets[0].Size != 0 {
		t.Errorf("size must be 0 when HEAD fails, got %d", rel.Assets[0].Size)
	}
}

func TestLatestReleaseErrors(t *testing.T) {
	newServer(t)

	_, err := LatestRelease("sysinternals/nope", "", false)
	if !errors.Is(err, github.ErrNotFound) || !strings.Contains(err.Error(), "process-explorer") {
		t.Errorf("unknown tool must match ErrNotFound and hint at the slug, got %v", err)
	}
	if _, err := LatestRelease("sysinternals/empty", "", false); err == nil {
		t.Error("a page without a download link must fail")
	}
}

func TestReleaseByTag(t *testing.T) {
	newServer(t)

	if rel, err := ReleaseByTag("sysinternals/process-explorer", "v17.2-2026-10-15", "", false); err != nil || rel.TagName != "v17.2-2026-10-15" {
		t.Fatalf("current build: err=%v rel=%+v", err, rel)
	}
	if _, err := ReleaseByTag("sysinternals/process-explorer", "v17.1-2026-01-01", "", false); !errors.Is(err, github.ErrNotFound) {
		t.Errorf("an old build must match ErrNotFound, got %v", err)
	}
}
