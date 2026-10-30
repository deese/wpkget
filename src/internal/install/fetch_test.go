package install

import (
	"archive/zip"
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/deese/wpkget/src/internal/github"
)

// zipBytes builds an in-memory zip from name -> content.
func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fetchEnv serves one zip asset and stubs the release lookups. It returns the
// number of downloads served and the tags that were requested.
type fetchEnv struct {
	downloads atomic.Int32
	tagsAsked []string
	latestHit bool
}

func newFetchEnv(t *testing.T, files map[string]string, knownTags ...string) *fetchEnv {
	t.Helper()
	env := &fetchEnv{}
	data := zipBytes(t, files)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.downloads.Add(1)
		w.Write(data)
	}))
	t.Cleanup(srv.Close)

	release := func(tag string) *github.Release {
		return &github.Release{TagName: tag, Assets: []github.Asset{{
			Name: "tool_windows_amd64.zip", BrowserDownloadURL: srv.URL + "/a.zip", Size: int64(len(data)),
		}}}
	}

	oldLatest, oldByTag := getLatest, getByTag
	t.Cleanup(func() { getLatest, getByTag = oldLatest, oldByTag })
	getLatest = func(provider, host, repo, ua string, dbg bool) (*github.Release, error) {
		env.latestHit = true
		return release("v9.9.9"), nil
	}
	getByTag = func(provider, host, repo, tag, ua string, dbg bool) (*github.Release, error) {
		env.tagsAsked = append(env.tagsAsked, tag)
		for _, k := range knownTags {
			if k == tag {
				return release(tag), nil
			}
		}
		return nil, github.NewNotFound("no such tag")
	}
	return env
}

func baseOpts(out string) Options {
	return Options{Repo: "owner/tool", BinDir: out, Match: "*.zip"}
}

func TestFetchExtractsTheBinaryAndTracksNothing(t *testing.T) {
	env := newFetchEnv(t, map[string]string{"tool.exe": "BIN", "README.md": "docs"})
	out := t.TempDir()

	res, err := Fetch(baseOpts(out))
	if err != nil {
		t.Fatal(err)
	}
	if !env.latestHit || res.Version != "v9.9.9" {
		t.Fatalf("without --tag the latest release must be used, got %q", res.Version)
	}
	if !filepath.IsAbs(res.BinaryPath) {
		t.Errorf("result path must be absolute: %s", res.BinaryPath)
	}
	if got, err := os.ReadFile(res.BinaryPath); err != nil || string(got) != "BIN" {
		t.Fatalf("binary not written: %v %q", err, got)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 1 {
		t.Errorf("only the binary must be written, got %d entries", len(entries))
	}
	if res.Size == 0 {
		t.Error("size must be reported")
	}
}

func TestFetchRefusesExistingDestinationUnlessForced(t *testing.T) {
	env := newFetchEnv(t, map[string]string{"tool.exe": "NEW"})
	out := t.TempDir()

	first, err := Fetch(baseOpts(out))
	if err != nil {
		t.Fatal(err)
	}
	before := env.downloads.Load()

	if _, err := Fetch(baseOpts(out)); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("existing destination must be refused, got %v", err)
	}
	if env.downloads.Load() != before {
		t.Error("the refusal must happen before downloading")
	}

	os.WriteFile(first.BinaryPath, []byte("OLD"), 0o755)
	opts := baseOpts(out)
	opts.Force = true
	if _, err := Fetch(opts); err != nil {
		t.Fatalf("--force must overwrite: %v", err)
	}
	if got, _ := os.ReadFile(first.BinaryPath); string(got) != "NEW" {
		t.Errorf("content = %q, want NEW", got)
	}
}

func TestFetchAllExtractsIntoASubdirectory(t *testing.T) {
	newFetchEnv(t, map[string]string{"tool.exe": "BIN", "docs/readme.txt": "R"}, "v1.2.3")
	out := t.TempDir()

	opts := baseOpts(out)
	opts.All, opts.Tag = true, "v1.2.3"
	res, err := Fetch(opts)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(out, "tool-v1.2.3"); !strings.EqualFold(res.BinaryPath, mustAbs(t, want)) {
		t.Fatalf("destination = %s, want %s", res.BinaryPath, want)
	}
	for _, rel := range []string{"tool.exe", filepath.Join("docs", "readme.txt")} {
		if _, err := os.Stat(filepath.Join(res.BinaryPath, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
	if _, err := Fetch(opts); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("a non-empty directory must be refused, got %v", err)
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestFetchNoExtractKeepsTheDownloadedFile(t *testing.T) {
	data := map[string]string{"tool.exe": "BIN"}
	newFetchEnv(t, data)
	out := t.TempDir()

	opts := baseOpts(out)
	opts.NoExtract = true
	res, err := Fetch(opts)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(res.BinaryPath) != "tool_windows_amd64.zip" {
		t.Fatalf("the archive must keep its name, got %s", res.BinaryPath)
	}
	if got, _ := os.ReadFile(res.BinaryPath); !bytes.Equal(got, zipBytes(t, data)) {
		t.Error("the archive must be saved unchanged")
	}
}

func TestFetchTagLookupTriesTheVPrefix(t *testing.T) {
	env := newFetchEnv(t, map[string]string{"tool.exe": "BIN"}, "v1.2.3")

	opts := baseOpts(t.TempDir())
	opts.Tag = "1.2.3" // the release is tagged v1.2.3
	res, err := Fetch(opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Version != "v1.2.3" {
		t.Errorf("version = %q", res.Version)
	}
	if strings.Join(env.tagsAsked, ",") != "1.2.3,v1.2.3" {
		t.Errorf("tags asked = %v", env.tagsAsked)
	}
	if env.latestHit {
		t.Error("--tag must not query the latest release")
	}

	opts.Tag = "v1.2.3" // and the other way around: exact tag, no retry needed
	opts.Force = true
	env.tagsAsked = nil
	if _, err := Fetch(opts); err != nil || len(env.tagsAsked) != 1 {
		t.Errorf("exact tag: err=%v asked=%v", err, env.tagsAsked)
	}
}

func TestFetchUnknownTagReportsBothAttempts(t *testing.T) {
	newFetchEnv(t, map[string]string{"tool.exe": "BIN"}, "v1.0.0")
	opts := baseOpts(t.TempDir())
	opts.Tag = "2.0.0"

	_, err := Fetch(opts)
	if !errors.Is(err, github.ErrNotFound) || !strings.Contains(err.Error(), `also tried "v2.0.0"`) {
		t.Fatalf("got %v", err)
	}
}

func TestFetchDryRunWritesNothing(t *testing.T) {
	env := newFetchEnv(t, map[string]string{"tool.exe": "BIN"})
	out := t.TempDir()
	opts := baseOpts(out)
	opts.DryRun = true

	if _, err := Fetch(opts); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Errorf("dry-run created %d entries", len(entries))
	}
	if env.downloads.Load() != 0 {
		t.Error("dry-run must not download")
	}
}

func TestSanitizeName(t *testing.T) {
	if got := sanitizeName(`a/b\c:d`); got != "a_b_c_d" {
		t.Errorf("sanitizeName = %q", got)
	}
}

func TestWithProviderDefaults(t *testing.T) {
	if !withProviderDefaults(Options{Provider: "sysinternals"}).All {
		t.Error("sysinternals must extract everything by default")
	}
	if withProviderDefaults(Options{Provider: "sysinternals", BinaryName: "x"}).All {
		t.Error("--name asks for a single binary, so All must stay off")
	}
	if withProviderDefaults(Options{Provider: "github"}).All {
		t.Error("other providers must be left alone")
	}
}
