package main

import (
	"testing"
	"time"

	"github.com/deese/wpkget/src/internal/config"
	"github.com/deese/wpkget/src/internal/packages"
)

func TestEffectiveMinAge(t *testing.T) {
	cfg := &config.Config{MinReleaseAge: "48h"}
	pkg := &packages.Entry{MinReleaseAge: "6h"}
	off := &packages.Entry{MinReleaseAge: "0"}

	tests := []struct {
		name  string
		g     globals
		entry *packages.Entry
		want  time.Duration
	}{
		{"global applies", globals{}, nil, 48 * time.Hour},
		{"package overrides global", globals{}, pkg, 6 * time.Hour},
		{"package can disable the global minimum", globals{}, off, 0},
		{"flag overrides package", globals{minAgeSet: true, minAgeValue: time.Hour}, pkg, time.Hour},
		{"flag 0 disables everything", globals{minAgeSet: true}, pkg, 0},
		{"unset package falls back to global", globals{}, &packages.Entry{}, 48 * time.Hour},
	}
	for _, tt := range tests {
		if got := effectiveMinAge(cfg, &tt.g, tt.entry); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}
