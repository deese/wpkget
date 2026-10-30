package gitlab

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deese/wpkget/src/internal/config"
	"github.com/deese/wpkget/src/internal/github"
)

const releasesJSON = `[{"tag_name":"v1.2.3","assets":{"links":[{"name":"tool.zip","url":"https://example.com/tool.zip"}]}}]`

// newTLSServer starts an HTTPS server and returns it with the PEM of its certificate.
func newTLSServer(t *testing.T, h http.Handler) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)

	caPath := filepath.Join(t.TempDir(), "ca.pem")
	pemBytes := encodeCert(srv.Certificate().Raw)
	if err := os.WriteFile(caPath, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return srv, caPath
}

func hostOf(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

func TestLatestRelease_TokenFromEnvAndCAFile(t *testing.T) {
	var gotToken string
	srv, caPath := newTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		w.Write([]byte(releasesJSON))
	}))
	host := hostOf(t, srv)

	t.Setenv("TEST_GL_TOKEN", "secret")
	t.Setenv("TEST_GL_CA", caPath)
	Configure(&config.Config{GitLabHosts: map[string]config.GitLabHost{
		host: {TokenEnv: "TEST_GL_TOKEN", CAFile: "${TEST_GL_CA}"},
	}})

	rel, err := LatestRelease(host, "grp/sub/proj", "wpkget-test", false)
	if err != nil {
		t.Fatal(err)
	}
	if rel.TagName != "v1.2.3" || len(rel.Assets) != 1 {
		t.Fatalf("unexpected release: %+v", rel)
	}
	if gotToken != "secret" {
		t.Fatalf("token not sent, got %q", gotToken)
	}
}

func TestLatestRelease_CAFromWpkgetEnv(t *testing.T) {
	srv, caPath := newTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(releasesJSON))
	}))
	host := hostOf(t, srv)

	t.Setenv("WPKGET_CA_FILE", caPath)
	Configure(&config.Config{GitLabHosts: map[string]config.GitLabHost{host: {}}})

	if _, err := LatestRelease(host, "grp/proj", "", false); err != nil {
		t.Fatal(err)
	}
}

func TestLatestRelease_UntrustedCertFailsUnlessSkipVerify(t *testing.T) {
	srv, _ := newTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(releasesJSON))
	}))
	host := hostOf(t, srv)
	t.Setenv("WPKGET_CA_FILE", "")

	Configure(&config.Config{GitLabHosts: map[string]config.GitLabHost{host: {}}})
	if _, err := LatestRelease(host, "grp/proj", "", false); err == nil {
		t.Fatal("expected TLS error without ca_file")
	}

	Configure(&config.Config{GitLabHosts: map[string]config.GitLabHost{host: {SkipVerify: true}}})
	if _, err := LatestRelease(host, "grp/proj", "", false); err != nil {
		t.Fatalf("skip_verify should succeed: %v", err)
	}
}

func TestLatestRelease_UnconfiguredHostRejected(t *testing.T) {
	Configure(&config.Config{})
	_, err := LatestRelease("git.unknown.example", "grp/proj", "", false)
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected not-configured error, got %v", err)
	}
}

func TestPrepare_UnknownHostGetsNoToken(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "must-not-leak")
	Configure(&config.Config{})
	req, _ := http.NewRequest(http.MethodGet, "https://other.example/file.zip", nil)
	client, err := Prepare(req)
	if err != nil || client != nil {
		t.Fatalf("expected nil client and no error, got %v, %v", client, err)
	}
	if req.Header.Get("PRIVATE-TOKEN") != "" {
		t.Fatal("token leaked to unknown host")
	}
}

func TestRedirectToOtherHostDropsToken(t *testing.T) {
	var leaked string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("PRIVATE-TOKEN")
	}))
	t.Cleanup(other.Close)

	srv, caPath := newTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	host := hostOf(t, srv)

	t.Setenv("TEST_GL_TOKEN", "secret")
	Configure(&config.Config{GitLabHosts: map[string]config.GitLabHost{
		host: {TokenEnv: "TEST_GL_TOKEN", CAFile: caPath},
	}})

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	client, err := Prepare(req)
	if err != nil || client == nil {
		t.Fatalf("Prepare: %v, %v", client, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if leaked != "" {
		t.Fatalf("token leaked on cross-host redirect: %q", leaked)
	}
}

func TestTokenFromPass(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fakepass")
	body := "#!/bin/sh\nprintf 'tok-from-pass\nsecond line\n'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no POSIX shell available")
	}
	Configure(&config.Config{PassCmd: script})
	tok, err := resolveToken(config.GitLabHost{TokenPass: "gitlab/example"})
	if err != nil {
		t.Fatal(err)
	}
	if tok != "tok-from-pass" {
		t.Fatalf("got %q", tok)
	}
}

func TestPrefix(t *testing.T) {
	var gotPath string
	srv, caPath := newTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.Write([]byte(releasesJSON))
	}))
	host := hostOf(t, srv)
	Configure(&config.Config{GitLabHosts: map[string]config.GitLabHost{
		host: {Prefix: "gitlab/", CAFile: caPath},
	}})

	if _, err := LatestRelease(host, "grp/sub/proj", "", false); err != nil {
		t.Fatal(err)
	}
	if want := "/gitlab/api/v4/projects/grp%2Fsub%2Fproj/releases"; gotPath != want {
		t.Fatalf("path = %q, want %q", gotPath, want)
	}
	if got := TrimPrefix(host, "gitlab/grp/proj"); got != "grp/proj" {
		t.Fatalf("TrimPrefix = %q", got)
	}
	if got := TrimPrefix(host, "grp/proj"); got != "grp/proj" {
		t.Fatalf("TrimPrefix without prefix = %q", got)
	}
}

func TestReleaseByTag(t *testing.T) {
	var gotPath string
	srv, caPath := newTLSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		if strings.HasSuffix(gotPath, "/releases/v9.9.9") {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"tag_name":"v1.0.0","assets":{"links":[{"name":"tool.zip","url":"https://example.com/tool.zip"}]}}`))
	}))
	host := hostOf(t, srv)
	Configure(&config.Config{GitLabHosts: map[string]config.GitLabHost{host: {CAFile: caPath}}})

	rel, err := ReleaseByTag(host, "grp/sub/proj", "v1.0.0", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if want := "/api/v4/projects/grp%2Fsub%2Fproj/releases/v1.0.0"; gotPath != want {
		t.Fatalf("path = %q, want %q", gotPath, want)
	}
	if rel.TagName != "v1.0.0" || len(rel.Assets) != 1 {
		t.Fatalf("unexpected release: %+v", rel)
	}

	_, err = ReleaseByTag(host, "grp/sub/proj", "v9.9.9", "", false)
	if !errors.Is(err, github.ErrNotFound) {
		t.Fatalf("a missing tag must match ErrNotFound, got %v", err)
	}
}
