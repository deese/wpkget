package config

import (
	"testing"
	"time"
)

func TestParseMinAge(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", 0, false},
		{"0", 0, false},
		{"48h", 48 * time.Hour, false},
		{"90m", 90 * time.Minute, false},
		{"2d", 0, true}, // days are deliberately unsupported
		{"-1h", 0, true},
		{"abc", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseMinAge(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseMinAge(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseMinAge(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
