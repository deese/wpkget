package main

import (
	"strconv"
	"strings"
)

// isOlderVersion reports whether tag a is confidently older than tag b. It
// understands dotted numeric tags with an optional "v" prefix and pre-release
// suffix ("v1.2.3", "1.2.0-rc.612"). It returns false whenever the order cannot
// be established (non-numeric tags, unrelated suffixes), so it only ever
// blocks a downgrade it is sure about.
func isOlderVersion(a, b string) bool {
	coreA, preA, okA := splitVersion(a)
	coreB, preB, okB := splitVersion(b)
	if !okA || !okB {
		return false
	}
	if c := compareNumbers(coreA, coreB); c != 0 {
		return c < 0
	}
	switch {
	case preA == nil && preB == nil:
		return false
	case preA == nil: // a release is newer than its own pre-release
		return false
	case preB == nil:
		return true
	}
	return compareNumbers(preA, preB) < 0
}

// splitVersion splits a tag into its numeric core and the numbers found in its
// pre-release suffix (nil when there is none). ok is false when the core is
// not made of dot-separated numbers.
func splitVersion(tag string) (core, pre []int, ok bool) {
	tag = strings.TrimPrefix(strings.TrimSpace(tag), "v")
	tag, _, _ = strings.Cut(tag, "+") // build metadata does not affect the order
	coreStr, preStr, hasPre := strings.Cut(tag, "-")
	if coreStr == "" {
		return nil, nil, false
	}
	for _, part := range strings.Split(coreStr, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, nil, false
		}
		core = append(core, n)
	}
	if hasPre {
		pre = []int{}
		for _, field := range strings.FieldsFunc(preStr, func(r rune) bool { return r < '0' || r > '9' }) {
			if n, err := strconv.Atoi(field); err == nil {
				pre = append(pre, n)
			}
		}
	}
	return core, pre, true
}

// compareNumbers compares two number lists, padding the shorter with zeros.
func compareNumbers(a, b []int) int {
	for i := 0; i < max(len(a), len(b)); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
