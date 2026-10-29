// Package progress renders a single-line download progress bar using only
// the standard library.
package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Display modes, selectable with --progress and the "progress" config key.
const (
	Auto   = "auto"   // draw only when the output is a terminal
	Always = "always" // draw unconditionally (e.g. Git Bash/mintty, which is not detected as a terminal)
	Never  = "never"  // never draw
)

const (
	labelWidth  = 24
	barWidth    = 20
	minInterval = 100 * time.Millisecond
)

// ValidMode reports whether s is an accepted mode.
func ValidMode(s string) bool {
	return s == Auto || s == Always || s == Never
}

// Enabled reports whether a bar should be drawn on f for the given mode.
// Auto only enables it for character devices, so logs and pipes stay clean.
func Enabled(mode string, f *os.File) bool {
	switch mode {
	case Always:
		return true
	case Auto, "":
		info, err := f.Stat()
		return err == nil && info.Mode()&os.ModeCharDevice != 0
	default:
		return false
	}
}

// Reader wraps an io.Reader and draws progress to w while it is consumed.
type Reader struct {
	r        io.Reader
	w        io.Writer
	label    string
	total    int64 // <= 0 when unknown
	done     int64
	start    time.Time
	last     time.Time
	lastLen  int
	drawn    bool
	finished bool
	now      func() time.Time // replaced in tests
}

// NewReader returns a Reader that reports progress of r on w. total is the
// expected size in bytes, or <= 0 when unknown.
func NewReader(r io.Reader, w io.Writer, label string, total int64) *Reader {
	p := &Reader{r: r, w: w, label: label, total: total, now: time.Now}
	p.start = p.now()
	return p
}

// Read implements io.Reader.
func (p *Reader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if now := p.now(); now.Sub(p.last) >= minInterval {
		p.last = now
		p.draw(false)
	}
	return n, err
}

// Finish draws the final summary line. Call it after a successful copy.
func (p *Reader) Finish() {
	if p.finished {
		return
	}
	p.finished = true
	p.draw(true)
}

// Close ends the bar line if the transfer was aborted, so later output does
// not land on top of it. It is a no-op after Finish.
func (p *Reader) Close() {
	if p.finished {
		return
	}
	p.finished = true
	if p.drawn {
		fmt.Fprintln(p.w)
	}
}

func (p *Reader) draw(final bool) {
	line := Render(p.label, p.done, p.total, p.now().Sub(p.start), final)
	pad := ""
	if n := p.lastLen - len(line); n > 0 {
		pad = strings.Repeat(" ", n)
	}
	end := ""
	if final {
		end = "\n"
	}
	fmt.Fprintf(p.w, "\r%s%s%s", line, pad, end)
	p.lastLen = len(line)
	p.drawn = true
}

// Render builds one progress line. total <= 0 means the size is unknown.
// The final line replaces the live counters with total size and elapsed time.
func Render(label string, done, total int64, elapsed time.Duration, final bool) string {
	name := fit(label, labelWidth)

	if total <= 0 {
		if final {
			return fmt.Sprintf("%s  %s  %s", name, formatBytes(done), formatDuration(elapsed))
		}
		return fmt.Sprintf("%s  %s  %s", name, formatBytes(done), speed(done, elapsed))
	}

	frac := float64(done) / float64(total)
	if frac > 1 {
		frac = 1
	}
	filled := int(frac * barWidth)
	bar := strings.Repeat("#", filled) + strings.Repeat("-", barWidth-filled)
	if final {
		return fmt.Sprintf("%s  [%s] %3d%%  %s  %s", name, bar, int(frac*100), formatBytes(done), formatDuration(elapsed))
	}
	return fmt.Sprintf("%s  [%s] %3d%%  %s  %s", name, bar, int(frac*100), formatPair(done, total), speed(done, elapsed))
}

// fit pads or shortens s to exactly width runes, cutting from the middle.
func fit(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s + strings.Repeat(" ", width-len(r))
	}
	keep := width - 3
	head := (keep + 1) / 2
	tail := keep / 2
	return string(r[:head]) + "..." + string(r[len(r)-tail:])
}

var units = []string{"B", "KiB", "MiB", "GiB", "TiB"}

// unitFor returns the index into units that suits n.
func unitFor(n int64) int {
	i := 0
	for v := float64(n); v >= 1024 && i < len(units)-1; v /= 1024 {
		i++
	}
	return i
}

func scaled(n int64, unit int) float64 {
	v := float64(n)
	for ; unit > 0; unit-- {
		v /= 1024
	}
	return v
}

func formatBytes(n int64) string {
	u := unitFor(n)
	if u == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", scaled(n, u), units[u])
}

// formatPair renders "done/total UNIT" using the unit of total, e.g. "2.1/4.7 MiB".
func formatPair(done, total int64) string {
	u := unitFor(total)
	if u == 0 {
		return fmt.Sprintf("%d/%d B", done, total)
	}
	return fmt.Sprintf("%.1f/%.1f %s", scaled(done, u), scaled(total, u), units[u])
}

func speed(done int64, elapsed time.Duration) string {
	if elapsed <= 0 {
		return "- /s"
	}
	return formatBytes(int64(float64(done)/elapsed.Seconds())) + "/s"
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}
