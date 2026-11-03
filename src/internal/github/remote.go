package github

import (
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// maxRemoteBody caps how much of a remote response is read into memory.
const maxRemoteBody = 8 << 20

// maxFeedCandidates is how many of the newest feed entries are tried when
// looking for the latest release that has assets.
const maxFeedCandidates = 10

// remoteBase is the base URL of a generic proxy (e.g. an Artifactory generic
// remote) in front of https://github.com/. Empty means talk to api.github.com.
var remoteBase string

// assetHref matches the download links in a release's expanded_assets page.
var assetHref = regexp.MustCompile(`href="[^"]*/releases/download/[^/"]+/([^"?#]+)"`)

// atomFeed is the subset of releases.atom needed to find release tags.
type atomFeed struct {
	Entries []struct {
		Updated string `xml:"updated"`
		Links   []struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

// remoteFeed returns the releases listed in the repo's releases.atom feed,
// newest first, with only TagName and PublishedAt filled in. The feed carries
// the last update time of each release, which moves forward when a release is
// edited; that can only delay a release, never make it look older.
func remoteFeed(repo, userAgent string, debugHTTP bool) ([]Release, error) {
	feedURL := fmt.Sprintf("%s/%s/releases.atom", remoteBase, repo)
	body, err := remoteGet(feedURL, userAgent, debugHTTP,
		fmt.Sprintf("github: repository %q not found or has no releases", repo))
	if err != nil {
		return nil, err
	}

	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("github: decode releases feed: %w", err)
	}

	const marker = "/releases/tag/"
	var releases []Release
	for _, entry := range feed.Entries {
		for _, link := range entry.Links {
			_, escaped, ok := strings.Cut(link.Href, marker)
			if !ok || escaped == "" {
				continue
			}
			tag, err := url.PathUnescape(escaped)
			if err != nil {
				continue
			}
			updated, _ := time.Parse(time.RFC3339, strings.TrimSpace(entry.Updated)) // zero when unparsable
			releases = append(releases, Release{TagName: tag, PublishedAt: updated})
			break
		}
	}
	return releases, nil
}

// remoteLatestRelease resolves the latest release through the remote. A generic
// remote only proxies files from github.com (no API, and it swallows the
// redirect of /releases/latest), so the tag is taken from the newest entry of
// the releases.atom feed that has assets. Unlike the API, the feed may include
// pre-releases.
func remoteLatestRelease(repo, userAgent string, debugHTTP bool) (*Release, error) {
	feed, err := remoteFeed(repo, userAgent, debugHTTP)
	if err != nil {
		return nil, err
	}
	// Some repos publish releases without assets (e.g. ripgrep tags its helper
	// crates as "ignore-0.4.34"), so try the newest tags until one has assets.
	for i, entry := range feed {
		if i >= maxFeedCandidates {
			return nil, NewNotFound(fmt.Sprintf("github: none of the %d newest releases of %q has assets", i, repo))
		}
		release, err := remoteReleaseByTag(repo, entry.TagName, userAgent, debugHTTP)
		if err == nil || !errors.Is(err, ErrNotFound) {
			return release, err
		}
	}
	return nil, NewNotFound(fmt.Sprintf("github: repository %q not found or has no releases with assets", repo))
}

// remoteReleaseOlderThan is ReleaseOlderThan for the remote: it picks the tag
// from the feed (see PickEligible) and skips releases without assets.
func remoteReleaseOlderThan(repo string, minAge time.Duration, current, userAgent string, debugHTTP bool) (*Release, error) {
	feed, err := remoteFeed(repo, userAgent, debugHTTP)
	if err != nil {
		return nil, err
	}
	for range maxFeedCandidates {
		picked, err := PickEligible(feed, minAge, current, time.Now())
		if err != nil {
			return nil, fmt.Errorf("github: %q: %w", repo, err)
		}
		release, err := remoteReleaseByTag(repo, picked.TagName, userAgent, debugHTTP)
		if err == nil {
			release.PublishedAt = picked.PublishedAt
			return release, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		// No assets (e.g. a helper crate release): look at the next candidate.
		feed = slices.DeleteFunc(feed, func(r Release) bool { return r.TagName == picked.TagName })
	}
	return nil, NewNotFound(fmt.Sprintf("github: none of the %d eligible releases of %q has assets", maxFeedCandidates, repo))
}

// remoteReleaseByTag lists the assets of a tag from its expanded_assets page.
// The page carries no size or digest, so downloads are not verified.
func remoteReleaseByTag(repo, tag, userAgent string, debugHTTP bool) (*Release, error) {
	escapedTag := url.PathEscape(tag)
	pageURL := fmt.Sprintf("%s/%s/releases/expanded_assets/%s", remoteBase, repo, escapedTag)
	body, err := remoteGet(pageURL, userAgent, debugHTTP,
		fmt.Sprintf("github: no release with tag %q in %q", tag, repo))
	if err != nil {
		return nil, err
	}

	release := &Release{TagName: tag}
	seen := map[string]bool{}
	for _, m := range assetHref.FindAllStringSubmatch(string(body), -1) {
		name, err := url.PathUnescape(html.UnescapeString(m[1]))
		if err != nil || name == "" || seen[name] {
			continue
		}
		seen[name] = true
		release.Assets = append(release.Assets, Asset{
			Name: name,
			BrowserDownloadURL: fmt.Sprintf("%s/%s/releases/download/%s/%s",
				remoteBase, repo, escapedTag, url.PathEscape(name)),
		})
	}
	if len(release.Assets) == 0 {
		return nil, NewNotFound(fmt.Sprintf("github: no release with tag %q in %q (or it has no assets)", tag, repo))
	}
	return release, nil
}

// remoteGet fetches a URL from the remote and returns the body. HTTP 404 maps
// to a not-found error with the given message. No GitHub token is sent: the
// remote is a different trust domain.
func remoteGet(rawURL, userAgent string, debugHTTP bool, notFound string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	if debugHTTP {
		dump, _ := httputil.DumpRequestOut(req, true)
		log.Printf("debug-http: request:\n%s", dump)
	}

	resp, err := httpClient.Do(req)
	if debugHTTP && resp != nil {
		dump, _ := httputil.DumpResponse(resp, false)
		log.Printf("debug-http: response:\n%s", dump)
	}
	if err != nil {
		return nil, fmt.Errorf("github: remote request failed: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, NewNotFound(notFound)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("github: remote returned status %d for %s", resp.StatusCode, rawURL)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRemoteBody))
	if err != nil {
		return nil, fmt.Errorf("github: read remote response: %w", err)
	}
	return body, nil
}
