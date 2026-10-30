package main

import (
	"os"
	"strconv"

	"github.com/deese/wpkget/src/internal/config"
	"github.com/deese/wpkget/src/internal/progress"
	"github.com/deese/wpkget/src/internal/ui"
)

const (
	headerTitle     = "wpkget"
	minVersionWidth = 10
)

// optBool is a bool flag that also records whether it was given at all, so a
// CLI value can override a config default in both directions.
type optBool struct{ set, val bool }

func (b *optBool) String() string {
	if !b.set {
		return ""
	}
	return strconv.FormatBool(b.val)
}

func (b *optBool) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	b.set, b.val = true, v
	return nil
}

// IsBoolFlag lets the flag be used without a value (--modern).
func (b *optBool) IsBoolFlag() bool { return true }

// newUI returns the modern renderer when it applies, or nil for the classic
// output. names and versions size the columns before the first row is printed.
//
// The mode comes from modern_term in the config, overridden by --modern /
// --no-modern. A configured (not forced) modern mode falls back to the classic
// output when stdout is not a terminal, so logs and pipes stay stable.
func newUI(cfg *config.Config, g *globals, names, versions []string) *ui.UI {
	want, forced := cfg.ModernTerm, false
	if g.modern.set {
		want, forced = g.modern.val, g.modern.val
	}
	on, opts := ui.Detect(want, forced, os.Stdout)
	if !on {
		return nil
	}

	opts.Quiet = g.quiet
	// The live line shares the terminal with verbose/debug logs and is turned
	// off by --progress never.
	opts.Live = !(g.verbose || g.debug || g.debugHTTP) && cfg.Progress != progress.Never

	for _, n := range names {
		opts.NameWidth = max(opts.NameWidth, len([]rune(n)))
	}
	opts.VersionWidth = minVersionWidth
	for _, v := range versions {
		opts.VersionWidth = max(opts.VersionWidth, len([]rune(ui.CleanVersion(v))))
	}
	return ui.New(os.Stdout, opts)
}
