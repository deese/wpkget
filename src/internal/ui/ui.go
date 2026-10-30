// Package ui renders the optional "modern" terminal output: colored status
// rows with aligned columns, a live progress line and a final summary.
// It uses only the standard library.
package ui

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/deese/wpkget/src/internal/progress"
)

// Status is the outcome of processing one package.
type Status int

const (
	UpToDate Status = iota
	Updated         // also used for fresh installs (Old empty)
	Pinned
	Failed
)

// Result describes the outcome for one package. For UpToDate and Pinned, New
// holds the current version and Old is empty.
type Result struct {
	Repo   string
	Old    string
	New    string
	Status Status
	Size   int64         // bytes downloaded (Updated)
	Dur    time.Duration // download time (Updated)
	DryRun bool          // Updated: nothing was actually installed
	Err    error         // Failed
}

// Options configures a UI.
type Options struct {
	Color        bool // emit ANSI colors
	ASCII        bool // use ASCII icons instead of Unicode
	Live         bool // draw the waiting counter and progress line
	Quiet        bool // hide up-to-date and pinned rows
	NameWidth    int  // width of the package column
	VersionWidth int  // minimum width of the version columns
}

type iconSet struct {
	ok, upd, pin, fail, spin, full, empty, arrow, rule string
}

var (
	unicodeIcons = iconSet{"✓", "▲", "•", "✗", "⟳", "█", "░", "→", "─"}
	asciiIcons   = iconSet{"+", "^", "o", "x", "*", "#", "-", "->", "-"}
)

const (
	liveNameWidth = 36
	barWidth      = 20
	ruleWidth     = 46
	clearLine     = "\r\x1b[2K"
)

// UI writes the modern output to a single writer.
type UI struct {
	w     io.Writer
	o     Options
	ic    iconSet
	start time.Time

	updated, upToDate, pinned, failed int
	totalSize                         int64
	totalDur                          time.Duration
}

// New returns a UI that writes to w.
func New(w io.Writer, o Options) *UI {
	ic := unicodeIcons
	if o.ASCII {
		ic = asciiIcons
	}
	return &UI{w: w, o: o, ic: ic, start: time.Now()}
}

// Detect decides whether the modern output should be used and which terminal
// capabilities it can rely on. want is the configured/requested mode; forced
// means the user asked for it explicitly (--modern), which skips terminal
// detection so it also works in terminals Go cannot recognize, such as mintty.
// Without a terminal and without forcing, the classic output is kept so logs
// stay stable.
func Detect(want, forced bool, f *os.File) (bool, Options) {
	if !want {
		return false, Options{}
	}
	tty := isTerminal(f)
	if !tty && !forced {
		return false, Options{}
	}
	if tty && !enableVT(f) && !forced {
		return false, Options{} // console without ANSI support
	}
	_, noColor := os.LookupEnv("NO_COLOR")
	return true, Options{Color: !noColor, ASCII: preferASCII()}
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// preferASCII is true when the user asks for it (WPKGET_ASCII) or on a
// Windows console that is neither Windows Terminal nor an xterm-like emulator,
// where Unicode glyphs are likely to render as "?".
func preferASCII() bool {
	if os.Getenv("WPKGET_ASCII") != "" {
		return true
	}
	return runtime.GOOS == "windows" && os.Getenv("WT_SESSION") == "" && os.Getenv("TERM") == ""
}

func (u *UI) paint(code, s string) string {
	if !u.o.Color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (u *UI) dim(s string) string    { return u.paint("90", s) }
func (u *UI) green(s string) string  { return u.paint("32", s) }
func (u *UI) bgreen(s string) string { return u.paint("1;92", s) }
func (u *UI) yellow(s string) string { return u.paint("33", s) }
func (u *UI) red(s string) string    { return u.paint("31", s) }
func (u *UI) cyan(s string) string   { return u.paint("1;36", s) }
func (u *UI) bold(s string) string   { return u.paint("1", s) }

// pad right-pads s to n runes. It must run BEFORE coloring: ANSI escapes would
// otherwise count as visible characters and break the alignment.
func pad(s string, n int) string {
	if d := n - utf8.RuneCountInString(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

var verRe = regexp.MustCompile(`(?:^|[-_])(v?\d+\.\d+.*)$`)

// CleanVersion strips tag prefixes for display: "ipinfo-3.3.2" -> "3.3.2".
// Strings without a recognizable version are returned unchanged.
func CleanVersion(s string) string {
	if m := verRe.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return s
}

// Header prints the title line. Call it once before the first Row.
func (u *UI) Header(title, dir string) {
	fmt.Fprintf(u.w, "\n %s %s\n\n", u.cyan(u.ic.spin+" "+title), u.dim("· "+dir))
}

// Row prints the line for one package and updates the counters.
func (u *UI) Row(r Result) {
	nameW := u.o.NameWidth + 2
	verW := u.o.VersionWidth
	oldV, newV := CleanVersion(r.Old), CleanVersion(r.New)

	switch r.Status {
	case UpToDate:
		u.upToDate++
		if u.o.Quiet {
			return
		}
		fmt.Fprintf(u.w, " %s %s %s\n", u.green(u.ic.ok), u.dim(pad(r.Repo, nameW)), u.dim(newV))
	case Pinned:
		u.pinned++
		if u.o.Quiet {
			return
		}
		fmt.Fprintf(u.w, " %s %s %s  %s\n", u.dim(u.ic.pin), u.dim(pad(r.Repo, nameW)), u.dim(pad(newV, verW)), u.dim("pinned"))
	case Updated:
		u.updated++
		u.totalSize += r.Size
		u.totalDur += r.Dur
		from := pad("", verW) + " " + pad("", utf8.RuneCountInString(u.ic.arrow)) + " "
		if oldV != "" {
			from = u.red(pad(oldV, verW)) + " " + u.dim(u.ic.arrow) + " "
		}
		tail := u.dim(fmt.Sprintf("%10s  %6s", progress.FormatBytes(r.Size), progress.FormatDuration(r.Dur)))
		if r.DryRun {
			tail = u.dim("(dry-run)")
		}
		fmt.Fprintf(u.w, " %s %s %s%s  %s\n", u.yellow(u.ic.upd), u.bold(pad(r.Repo, nameW)), from, u.bgreen(pad(newV, verW)), tail)
	case Failed:
		u.failed++
		msg := "unknown error"
		if r.Err != nil {
			msg = r.Err.Error()
		}
		fmt.Fprintf(u.w, " %s %s\n     %s\n", u.red(u.ic.fail), u.bold(r.Repo), u.dim(msg))
	}
}

// Summary prints the totals line. Call it once after the last Row.
func (u *UI) Summary() {
	fmt.Fprintf(u.w, "\n %s\n ", u.dim(strings.Repeat(u.ic.rule, ruleWidth)))
	fmt.Fprintf(u.w, "%s  %s", u.yellow(u.bold(fmt.Sprintf("%s %d updated", u.ic.upd, u.updated))),
		u.green(fmt.Sprintf("%s %d up to date", u.ic.ok, u.upToDate)))
	if u.pinned > 0 {
		fmt.Fprintf(u.w, "  %s", u.dim(fmt.Sprintf("%s %d pinned", u.ic.pin, u.pinned)))
	}
	errs := fmt.Sprintf("%s %d errors", u.ic.fail, u.failed)
	if u.failed > 0 {
		errs = u.red(errs)
	} else {
		errs = u.dim(errs)
	}
	fmt.Fprintf(u.w, "  %s", errs)
	if u.updated > 0 && u.totalSize > 0 {
		fmt.Fprintf(u.w, "   %s", u.dim(fmt.Sprintf("%s · %s", progress.FormatBytes(u.totalSize), progress.FormatDuration(u.totalDur))))
	}
	fmt.Fprintln(u.w)
}
