package gitlab

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/deese/wpkget/src/internal/config"
	"github.com/deese/wpkget/src/internal/github"
	"github.com/deese/wpkget/src/internal/secret"
)

const (
	// DefaultHost is used when no host is given.
	DefaultHost = "gitlab.com"

	defaultTokenEnv = "GITLAB_TOKEN"
	caFileEnv       = "WPKGET_CA_FILE"
	tokenHeader     = "PRIVATE-TOKEN"
	clientTimeout   = 120 * time.Second
)

// session is the resolved HTTP client and credentials for one GitLab host.
type session struct {
	client *http.Client
	token  string
}

var (
	hosts    map[string]config.GitLabHost
	passCmd  string
	sessions = map[string]*session{}
)

type releaseJSON struct {
	TagName string `json:"tag_name"`
	Assets  struct {
		Links []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"links"`
	} `json:"assets"`
}

// Configure registers the self-hosted GitLab instances from the user config.
// It must be called before any other function in this package.
func Configure(cfg *config.Config) {
	hosts = make(map[string]config.GitLabHost, len(cfg.GitLabHosts))
	for h, hc := range cfg.GitLabHosts {
		hosts[strings.ToLower(h)] = hc
	}
	passCmd = cfg.PassCmd
	sessions = map[string]*session{}
}

// IsKnownHost reports whether host is gitlab.com or a configured instance.
func IsKnownHost(host string) bool {
	host = strings.ToLower(host)
	if host == DefaultHost {
		return true
	}
	_, ok := hosts[host]
	return ok
}

// Prepare adds the host credentials to req and returns the HTTP client that
// must be used to send it. It returns a nil client when req targets a host
// that is not a known GitLab, in which case the caller keeps its own client.
func Prepare(req *http.Request) (*http.Client, error) {
	host := strings.ToLower(req.URL.Host)
	if !IsKnownHost(host) {
		return nil, nil
	}
	s, err := sessionFor(host)
	if err != nil {
		return nil, err
	}
	if s.token != "" {
		req.Header.Set(tokenHeader, s.token)
	}
	return s.client, nil
}

// LatestRelease fetches the latest release for the given "namespace/project"
// from the given GitLab host (empty means gitlab.com).
func LatestRelease(host, repo, userAgent string, debugHTTP bool) (*github.Release, error) {
	if host == "" {
		host = DefaultHost
	}
	apiURL := fmt.Sprintf("https://%s%s/api/v4/projects/%s/releases?per_page=1", host, hostPrefix(host), url.PathEscape(repo))

	var releases []releaseJSON
	notFound := fmt.Sprintf("gitlab: repository %q not found or has no releases", repo)
	if err := getJSON(host, apiURL, repo, notFound, userAgent, debugHTTP, &releases); err != nil {
		return nil, err
	}
	if len(releases) == 0 {
		return nil, fmt.Errorf("gitlab: repository %q has no releases", repo)
	}
	return toRelease(releases[0]), nil
}

// ReleaseByTag fetches the release published for the given tag.
func ReleaseByTag(host, repo, tag, userAgent string, debugHTTP bool) (*github.Release, error) {
	if host == "" {
		host = DefaultHost
	}
	apiURL := fmt.Sprintf("https://%s%s/api/v4/projects/%s/releases/%s", host, hostPrefix(host), url.PathEscape(repo), url.PathEscape(tag))

	var r releaseJSON
	notFound := fmt.Sprintf("gitlab: no release with tag %q in %q", tag, repo)
	if err := getJSON(host, apiURL, repo, notFound, userAgent, debugHTTP, &r); err != nil {
		return nil, err
	}
	return toRelease(r), nil
}

// getJSON GETs apiURL with the credentials and TLS settings of host and
// decodes the JSON body into out. notFound is the message used for HTTP 404.
func getJSON(host, apiURL, repo, notFound, userAgent string, debugHTTP bool, out any) error {
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return fmt.Errorf("gitlab: build request: %w", err)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	client, err := Prepare(req)
	if err != nil {
		return err
	}
	if client == nil {
		return fmt.Errorf("gitlab: host %q is not configured (add it to gitlab_hosts)", host)
	}

	if debugHTTP {
		dump, _ := httputil.DumpRequestOut(req, true)
		log.Printf("debug-http: request:\n%s", dump)
	}

	resp, err := client.Do(req)
	if debugHTTP && resp != nil {
		dump, _ := httputil.DumpResponse(resp, false)
		log.Printf("debug-http: response:\n%s", dump)
	}
	if err != nil {
		return fmt.Errorf("gitlab: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return github.NewNotFound(notFound)
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("gitlab: access denied by %s, check the token (HTTP %d)", host, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gitlab: unexpected status %d for %s", resp.StatusCode, repo)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("gitlab: decode response: %w", err)
	}
	return nil
}

func toRelease(r releaseJSON) *github.Release {
	release := &github.Release{
		TagName: r.TagName,
		Assets:  make([]github.Asset, 0, len(r.Assets.Links)),
	}
	for _, link := range r.Assets.Links {
		release.Assets = append(release.Assets, github.Asset{
			Name:               link.Name,
			BrowserDownloadURL: link.URL,
		})
	}
	return release
}

// sessionFor returns the cached session for host, building it on first use.
func sessionFor(host string) (*session, error) {
	if s, ok := sessions[host]; ok {
		return s, nil
	}
	hc, configured := hosts[host]

	client, err := newClient(host, hc)
	if err != nil {
		return nil, err
	}
	var token string
	switch {
	case configured:
		token, err = resolveToken(hc)
	default:
		token = os.Getenv(defaultTokenEnv)
	}
	if err != nil {
		return nil, fmt.Errorf("gitlab: token for %s: %w", host, err)
	}

	s := &session{client: client, token: token}
	sessions[host] = s
	return s, nil
}

// newClient builds an HTTP client honoring the host's TLS settings. The
// token header is dropped on redirects to a different host so the token is
// never leaked to e.g. an object storage backend.
func newClient(host string, hc config.GitLabHost) (*http.Client, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}

	caFile := os.ExpandEnv(hc.CAFile)
	if caFile == "" {
		caFile = os.Getenv(caFileEnv)
	}
	if caFile != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("gitlab: read ca_file for %s: %w", host, err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("gitlab: ca_file %s for %s has no valid PEM certificates", caFile, host)
		}
		tlsCfg.RootCAs = pool
	}
	if hc.SkipVerify {
		log.Printf("warning: TLS verification disabled for %s (skip_verify)", host)
		tlsCfg.InsecureSkipVerify = true //nolint:gosec // explicit opt-in per host
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsCfg

	return &http.Client{
		Timeout:   clientTimeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
				req.Header.Del(tokenHeader)
			}
			return nil
		},
	}, nil
}

// resolveToken returns the access token for a configured host: first from the
// environment variable, then from the pass entry. An empty token is valid
// (public projects).
func resolveToken(hc config.GitLabHost) (string, error) {
	if hc.TokenEnv != "" {
		if v := os.Getenv(hc.TokenEnv); v != "" {
			return v, nil
		}
	}
	if hc.TokenPass != "" {
		return secret.FromPass(passCmd, hc.TokenPass)
	}
	return "", nil
}

// hostPrefix returns the normalized URL path prefix of host ("" or "/gitlab").
func hostPrefix(host string) string {
	p := strings.Trim(hosts[strings.ToLower(host)].Prefix, "/")
	if p == "" {
		return ""
	}
	return "/" + p
}

// TrimPrefix removes the configured URL path prefix of host from path
// (a path without leading slash). Paths lacking the prefix are returned as-is.
func TrimPrefix(host, path string) string {
	p := strings.TrimPrefix(hostPrefix(host), "/")
	if p == "" {
		return path
	}
	return strings.TrimPrefix(path, p+"/")
}

// HostStatus is the outcome of checking one configured host.
type HostStatus struct {
	Host     string
	HasToken bool
	Err      error // CA file or token lookup failure
}

// Check builds the session (CA bundle and token) of every configured host
// without contacting it, and reports what would fail. Token values are never
// exposed.
func Check(cfg *config.Config) []HostStatus {
	Configure(cfg)

	names := make([]string, 0, len(hosts))
	for h := range hosts {
		names = append(names, h)
	}
	sort.Strings(names)

	statuses := make([]HostStatus, 0, len(names))
	for _, h := range names {
		s, err := sessionFor(h)
		st := HostStatus{Host: h, Err: err}
		if err == nil {
			st.HasToken = s.token != ""
		}
		statuses = append(statuses, st)
	}
	return statuses
}
