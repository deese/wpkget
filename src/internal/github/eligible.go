package github

import (
	"fmt"
	"sort"
	"time"
)

// PickEligible returns the newest release that was published at least minAge
// before now, so a freshly published (and possibly malicious) release is not
// picked up before the community has had time to notice it.
//
// Drafts and pre-releases are skipped, and a release without a known
// publication date is never eligible. When current is the tag of the installed
// release and it is met before any eligible release, current is returned: it is
// already newer than (or equal to) what is allowed, and must not be downgraded.
func PickEligible(releases []Release, minAge time.Duration, current string, now time.Time) (*Release, error) {
	sorted := make([]Release, len(releases))
	copy(sorted, releases)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].PublishedAt.After(sorted[j].PublishedAt) })

	var newest *Release
	for i := range sorted {
		r := &sorted[i]
		if r.Draft || r.Prerelease {
			continue
		}
		if newest == nil {
			newest = r
		}
		if current != "" && r.TagName == current {
			return r, nil
		}
		if !r.PublishedAt.IsZero() && now.Sub(r.PublishedAt) >= minAge {
			return r, nil
		}
	}
	if newest == nil {
		return nil, NewNotFound("no releases found")
	}
	return nil, fmt.Errorf("no release is at least %s old (newest is %s, published %s)",
		minAge, newest.TagName, describeAge(newest.PublishedAt, now))
}

// describeAge renders how long ago t was, or "at an unknown date".
func describeAge(t, now time.Time) string {
	if t.IsZero() {
		return "at an unknown date"
	}
	return now.Sub(t).Round(time.Minute).String() + " ago"
}
