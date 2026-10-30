package main

import (
	"flag"
	"io"
	"testing"

	"github.com/deese/wpkget/src/internal/config"
)

func parseGlobals(t *testing.T, args ...string) globals {
	t.Helper()
	var g globals
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	registerGlobals(fs, &g)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return g
}

func TestModernFlagIsTriState(t *testing.T) {
	cases := []struct {
		args     []string
		set, val bool
	}{
		{nil, false, false},
		{[]string{"--modern"}, true, true},
		{[]string{"--modern=false"}, true, false},
		{[]string{"--no-modern"}, true, false},
		{[]string{"--modern", "--no-modern"}, true, false}, // last one wins
	}
	for _, tc := range cases {
		g := parseGlobals(t, tc.args...)
		if g.modern.set != tc.set || g.modern.val != tc.val {
			t.Errorf("%v: got set=%v val=%v, want set=%v val=%v", tc.args, g.modern.set, g.modern.val, tc.set, tc.val)
		}
	}
	if g := parseGlobals(t, "-q"); !g.quiet {
		t.Error("-q must set quiet")
	}
}

func TestNewUIDecision(t *testing.T) {
	// Under `go test` stdout is not a terminal.
	cfgOn := &config.Config{ModernTerm: true, Progress: "auto"}
	cfgOff := &config.Config{Progress: "auto"}

	cases := []struct {
		name string
		cfg  *config.Config
		args []string
		want bool
	}{
		{"default off", cfgOff, nil, false},
		{"configured but not a terminal: classic output", cfgOn, nil, false},
		{"forced on a non-terminal", cfgOff, []string{"--modern"}, true},
		{"--no-modern overrides the config", cfgOn, []string{"--no-modern"}, false},
	}
	for _, tc := range cases {
		g := parseGlobals(t, tc.args...)
		if got := newUI(tc.cfg, &g, []string{"a/b"}, []string{"v1.0.0"}) != nil; got != tc.want {
			t.Errorf("%s: modern=%v, want %v", tc.name, got, tc.want)
		}
	}
}
