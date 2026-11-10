package main

import "testing"

func TestIsOlderVersion(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"v10.5.0", "v10.5.2", true},
		{"v10.5.2", "v10.5.0", false},
		{"v0.74.5", "v0.75.0", true},
		{"v1.9", "v1.10.0", true},
		{"v1.0", "1.0.0", false},
		{"v1.2.3", "v1.2.3", false},
		{"1.2.0-rc.5", "1.2.0", true},
		{"1.2.0", "1.2.0-rc.5", false},
		{"1.2.0-rc.5", "1.2.0-rc.612", true},
		{"1.2.0-rc.612", "1.2.0-rc.5", false},
		{"v2.0.0", "v1.9.9", false},
		{"nightly", "v1.0.0", false},
		{"v1.0.0", "nightly", false},
		{"", "v1.0.0", false},
	}
	for _, tt := range tests {
		if got := isOlderVersion(tt.a, tt.b); got != tt.want {
			t.Errorf("isOlderVersion(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}
