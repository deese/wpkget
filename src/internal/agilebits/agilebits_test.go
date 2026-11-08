package agilebits

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deese/wpkget/src/internal/github"
)

// historyPage lists a stable build, a newer beta, an older stable build and a
// stable build whose only link points at another host.
const historyPage = `<main>
<article id="v2420001" >
<h3>
	2.42.0
	<span>(build <a href="#v2420001">#2420001</a>) – released 2026-11-03</span>
</h3>
<p class="system apple"><a href="https://cache.agilebits.com/dist/1P/op2/pkg/v2.42.0/op_apple_universal_v2.42.0.pkg">universal</a></p>
<p class="system linux">
	<a href="https://cache.agilebits.com/dist/1P/op2/pkg/v2.42.0/op_linux_amd64_v2.42.0.zip">amd64</a> |
	<a href="https://cache.agilebits.com/dist/1P/op2/pkg/v2.42.0/op_linux_amd64_v2.42.0.zip">amd64</a>
</p>
<p class="system windows">
	<a href="https://cache.agilebits.com/dist/1P/op2/pkg/v2.42.0/op_windows_amd64_v2.42.0.zip">amd64</a>
</p>
</article>
<article id="v2440001" class='beta' style='display:none;'>
<h3>
	2.44.0-beta.01
</h3>
<p><a href="https://cache.agilebits.com/dist/1P/op2/pkg/v2.44.0-beta.01/op_windows_amd64_v2.44.0-beta.01.zip">amd64</a></p>
</article>
<article id="v2410101" >
<h3>
	2.41.1
</h3>
<p><a href="https://cache.agilebits.com/dist/1P/op2/pkg/v2.41.1/op_windows_amd64_v2.41.1.zip">amd64</a></p>
</article>
<article id="v2390001" >
<h3>
	2.39.0
</h3>
<p><a href="https://evil.example.com/op_windows_amd64_v2.39.0.zip">amd64</a></p>
</article>
</main>`

const betaOnlyPage = `<article class='beta'><h3>1.0.0-beta.01</h3>
<a href="https://cache.agilebits.com/dist/x/op_linux_amd64_v1.0.0-beta.01.zip">amd64</a></article>`

func newServer(t *testing.T) {
	t.Helper()
	pages := map[string]string{
		"/history/CLI2":  historyPage,
		"/history/BETA":  betaOnlyPage,
		"/history/EMPTY": "<main>nothing</main>",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if page, ok := pages[r.URL.Path]; ok && r.Method == http.MethodGet {
			w.Write([]byte(page))
			return
		}
		if r.Method == http.MethodHead && strings.HasSuffix(r.URL.Path, "op_windows_amd64_v2.42.0.zip") {
			w.Header().Set("Content-Length", "16234486")
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	// Point the download links of the fixtures at the local server, so no test
	// reaches the real host.
	const realPrefix = "https://cache.agilebits.com/dist/"
	for path, page := range pages {
		pages[path] = strings.ReplaceAll(page, realPrefix, srv.URL+"/dist/")
	}
	oldHistory, oldPrefix := historyBase, downloadPrefix
	historyBase, downloadPrefix = srv.URL+"/history", srv.URL+"/dist/"
	t.Cleanup(func() { historyBase, downloadPrefix = oldHistory, oldPrefix })
}

func TestLatestReleaseSkipsBetas(t *testing.T) {
	newServer(t)

	rel, err := LatestRelease("agilebits/cli2", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "2.42.0" || rel.Prerelease {
		t.Errorf("tag = %q prerelease = %v, want the stable 2.42.0", rel.TagName, rel.Prerelease)
	}
	if len(rel.Assets) != 2 {
		t.Fatalf("assets = %+v, want the 2 distinct zips (no .pkg, no duplicate)", rel.Assets)
	}
	win := rel.Assets[1]
	wantURL := downloadPrefix + "1P/op2/pkg/v2.42.0/op_windows_amd64_v2.42.0.zip"
	if win.Name != "op_windows_amd64_v2.42.0.zip" || win.BrowserDownloadURL != wantURL || win.Size != 16234486 {
		t.Errorf("unexpected windows asset: %+v", win)
	}
	if rel.Assets[0].Size != 0 {
		t.Errorf("size must be 0 when HEAD fails, got %d", rel.Assets[0].Size)
	}
}

func TestLatestReleaseErrors(t *testing.T) {
	newServer(t)

	_, err := LatestRelease("agilebits/NOPE", "", false)
	if !errors.Is(err, github.ErrNotFound) || !strings.Contains(err.Error(), "CLI2") {
		t.Errorf("unknown product must match ErrNotFound and hint at the code, got %v", err)
	}
	if _, err := LatestRelease("agilebits/empty", "", false); err == nil {
		t.Error("a page without a download link must fail")
	}
	if _, err := LatestRelease("agilebits/beta", "", false); err == nil || !strings.Contains(err.Error(), "no stable") {
		t.Errorf("a page with only betas must fail with no stable release, got %v", err)
	}
}

func TestReleaseByTag(t *testing.T) {
	newServer(t)

	if rel, err := ReleaseByTag("agilebits/CLI2", "2.41.1", "", false); err != nil || rel.TagName != "2.41.1" {
		t.Fatalf("older stable: err=%v rel=%+v", err, rel)
	}
	rel, err := ReleaseByTag("agilebits/CLI2", "2.44.0-beta.01", "", false)
	if err != nil || !rel.Prerelease {
		t.Fatalf("beta by exact tag: err=%v rel=%+v", err, rel)
	}
	if _, err := ReleaseByTag("agilebits/CLI2", "9.9.9", "", false); !errors.Is(err, github.ErrNotFound) {
		t.Errorf("unknown version must match ErrNotFound, got %v", err)
	}
}

func TestForeignHostIsNeverFollowed(t *testing.T) {
	newServer(t)

	// 2.39.0 only links to another host, so it has no usable download.
	if _, err := ReleaseByTag("agilebits/CLI2", "2.39.0", "", false); !errors.Is(err, github.ErrNotFound) {
		t.Errorf("a version with only foreign links must not resolve, got %v", err)
	}
}
