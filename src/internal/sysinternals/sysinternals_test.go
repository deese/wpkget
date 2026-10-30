package sysinternals

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/deese/wpkget/src/internal/github"
)

const explorerDoc = `---
title: Process Explorer
ms.date: 10/15/2026
---

# Process Explorer v17.2

Published: October 15, 2026

[![Download](media/shared/Download_sm.png)](https://download.sysinternals.com/files/ProcessExplorer.zip) [**Download Process Explorer**](https://download.sysinternals.com/files/ProcessExplorer.zip)`

const suiteDoc = `---
ms.date: 09/01/2026
---

# Sysinternals Suite

[x64](https://download.sysinternals.com/files/SysinternalsSuite.zip)
[arm64](https://download.sysinternals.com/files/SysinternalsSuite-ARM64.zip)`

const pendMovesDoc = `---
ms.date: 09/17/2020
---

# PendMoves v1.3 and MoveFile v1.02

[Download](https://download.sysinternals.com/files/PendMove.zip)`

func newServer(t *testing.T) {
	t.Helper()
	docs := map[string]string{
		"/docs/process-explorer.md":   explorerDoc,
		"/docs/sysinternals-suite.md": suiteDoc,
		"/docs/pendmoves.md":          pendMovesDoc,
		"/docs/empty.md":              "# Nothing here v1.0\n",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if doc, ok := docs[r.URL.Path]; ok && r.Method == http.MethodGet {
			w.Write([]byte(doc))
			return
		}
		if r.Method == http.MethodHead && r.URL.Path == "/files/ProcessExplorer.zip" {
			w.Header().Set("Content-Length", "3813190")
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	oldDocs, oldDownload := docsBase, downloadBase
	docsBase, downloadBase = srv.URL+"/docs", srv.URL+"/files"
	t.Cleanup(func() { docsBase, downloadBase = oldDocs, oldDownload })
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

func TestLatestReleaseFirstVersionOfTitle(t *testing.T) {
	newServer(t)

	rel, err := LatestRelease("sysinternals/pendmoves", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "v1.3-2020-09-17" {
		t.Errorf("tag = %q, want the first version of the title", rel.TagName)
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
