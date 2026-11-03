package github

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/deese/wpkget/src/internal/config"
)

var eligibleNow = time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

func hoursAgo(h int) time.Time { return eligibleNow.Add(-time.Duration(h) * time.Hour) }

func TestPickEligible(t *testing.T) {
	releases := []Release{
		{TagName: "v3", PublishedAt: hoursAgo(1)},
		{TagName: "v2", PublishedAt: hoursAgo(60)},
		{TagName: "v1", PublishedAt: hoursAgo(500)},
	}
	tests := []struct {
		name    string
		in      []Release
		current string
		want    string
		wantErr bool
	}{
		{"skips a release that is too new", releases, "", "v2", false},
		{"update to the eligible release", releases, "v1", "v2", false},
		{"installed release newer than allowed is kept", releases, "v3", "v3", false},
		{"installed eligible release is kept", releases, "v2", "v2", false},
		{"input order does not matter", []Release{releases[2], releases[0], releases[1]}, "", "v2", false},
		{"drafts and pre-releases are skipped", []Release{
			{TagName: "pre", PublishedAt: hoursAgo(100), Prerelease: true},
			{TagName: "draft", PublishedAt: hoursAgo(90), Draft: true},
			{TagName: "v1", PublishedAt: hoursAgo(80)},
		}, "", "v1", false},
		{"unknown publication date is never eligible", []Release{{TagName: "v1"}}, "", "", true},
		{"nothing old enough", releases[:1], "", "", true},
		{"no releases", nil, "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := PickEligible(tt.in, 48*time.Hour, tt.current, eligibleNow)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("PickEligible: %v", err)
			}
			if got.TagName != tt.want {
				t.Errorf("tag = %q, want %q", got.TagName, tt.want)
			}
		})
	}
}

func TestReleaseOlderThanAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/releases" {
			http.NotFound(w, r)
			return
		}
		now := time.Now().UTC()
		w.Write([]byte(`[
{"tag_name":"v2","published_at":"` + now.Add(-time.Hour).Format(time.RFC3339) + `","assets":[{"name":"a.zip"}]},
{"tag_name":"v1","published_at":"` + now.Add(-100*time.Hour).Format(time.RFC3339) + `","assets":[{"name":"a.zip"}]}]`))
	}))
	t.Cleanup(srv.Close)
	old := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = old })

	rel, err := ReleaseOlderThan("o/r", 48*time.Hour, "", "", false)
	if err != nil {
		t.Fatalf("ReleaseOlderThan: %v", err)
	}
	if rel.TagName != "v1" || len(rel.Assets) != 1 {
		t.Errorf("got %+v, want v1 with its assets", rel)
	}
}

func TestRemoteReleaseOlderThan(t *testing.T) {
	now := time.Now().UTC()
	feed := `<feed xmlns="http://www.w3.org/2005/Atom">
<entry><updated>` + now.Add(-time.Hour).Format(time.RFC3339) + `</updated><link href="https://github.com/o/r/releases/tag/v1.2.3"/></entry>
<entry><updated>` + now.Add(-70*time.Hour).Format(time.RFC3339) + `</updated><link href="https://github.com/o/r/releases/tag/helper-1"/></entry>
<entry><updated>` + now.Add(-80*time.Hour).Format(time.RFC3339) + `</updated><link href="https://github.com/o/r/releases/tag/v1.2.2"/></entry></feed>`

	mux := http.NewServeMux()
	mux.HandleFunc("/o/r/releases.atom", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(feed)) })
	mux.HandleFunc("/o/r/releases/expanded_assets/helper-1", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`<ul></ul>`))
	})
	mux.HandleFunc("/o/r/releases/expanded_assets/v1.2.2", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`<a href="/o/r/releases/download/v1.2.2/r.zip">r.zip</a>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	Configure(&config.Config{GitHubRemote: srv.URL})
	t.Cleanup(func() { Configure(&config.Config{}) })

	// v1.2.3 is too new and helper-1 has no assets, so v1.2.2 is the answer.
	rel, err := ReleaseOlderThan("o/r", 48*time.Hour, "", "", false)
	if err != nil {
		t.Fatalf("ReleaseOlderThan: %v", err)
	}
	if rel.TagName != "v1.2.2" || len(rel.Assets) != 1 {
		t.Errorf("got %+v, want v1.2.2 with one asset", rel)
	}
}
