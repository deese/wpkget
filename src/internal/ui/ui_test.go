package ui

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func newUI(buf *bytes.Buffer, o Options) *UI {
	if o.NameWidth == 0 {
		o.NameWidth = 20
	}
	if o.VersionWidth == 0 {
		o.VersionWidth = 10
	}
	return New(buf, o)
}

func sample() []Result {
	return []Result{
		{Repo: "deese/tfdown", New: "v1.3.0", Status: UpToDate},
		{Repo: "can1357/oh-my-pi", Old: "v18.13.1", New: "v18.13.3", Status: Updated, Size: 245_000_000, Dur: 2400 * time.Millisecond},
		{Repo: "ipinfo/cli", Old: "ipinfo-3.3.2", New: "range2ip-3.3.4", Status: Updated, Size: 5 << 20, Dur: time.Second},
		{Repo: "neovim/neovim", New: "v0.10.4", Status: Pinned},
		{Repo: "foo/bar", Status: Failed, Err: errors.New("timeout")},
	}
}

func render(o Options, rs []Result) string {
	var buf bytes.Buffer
	u := newUI(&buf, o)
	u.Header("wpkget", `d:\bin`)
	for _, r := range rs {
		u.Row(r)
	}
	u.Summary()
	return buf.String()
}

func TestPlainOutputHasNoEscapes(t *testing.T) {
	out := render(Options{}, sample())
	if strings.Contains(out, "\x1b") {
		t.Fatalf("ANSI escapes with Color=false:\n%q", out)
	}
	for _, want := range []string{"deese/tfdown", "v18.13.1", "v18.13.3", "3.3.2", "3.3.4", "pinned", "timeout",
		"2 updated", "1 up to date", "1 pinned", "1 errors"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ipinfo-3.3.2") {
		t.Error("tag prefixes must be stripped for display")
	}
}

func TestColorOutputHasEscapes(t *testing.T) {
	if !strings.Contains(render(Options{Color: true}, sample()), "\x1b[") {
		t.Fatal("expected ANSI escapes with Color=true")
	}
}

func TestQuietHidesUpToDateAndPinnedButCountsThem(t *testing.T) {
	out := render(Options{Quiet: true}, sample())
	if strings.Contains(out, "deese/tfdown") || strings.Contains(out, "neovim/neovim") {
		t.Fatalf("quiet must hide up-to-date and pinned rows:\n%s", out)
	}
	if !strings.Contains(out, "1 up to date") || !strings.Contains(out, "1 pinned") {
		t.Fatalf("summary must still count them:\n%s", out)
	}
}

func TestColumnsAlignedByRunesWithAndWithoutColor(t *testing.T) {
	rs := []Result{
		{Repo: "a/b", New: "v1.0.0", Status: UpToDate},
		{Repo: "muy-largo/nombre-de-repo", New: "v2.0.0", Status: UpToDate},
	}
	for _, color := range []bool{false, true} {
		var buf bytes.Buffer
		u := newUI(&buf, Options{Color: color, NameWidth: 24})
		for _, r := range rs {
			u.Row(r)
		}
		var cols []int
		for _, l := range strings.Split(buf.String(), "\n") {
			if i := strings.Index(l, "v"); i > 0 && strings.Contains(l, "✓") {
				// visible width of everything before the version, ignoring escapes
				visible := stripANSI(l[:i])
				cols = append(cols, utf8.RuneCountInString(visible))
			}
		}
		if len(cols) != 2 || cols[0] != cols[1] {
			t.Fatalf("color=%v: misaligned columns %v", color, cols)
		}
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func TestFreshInstallRowAlignsWithUpdateRows(t *testing.T) {
	out := render(Options{}, []Result{
		{Repo: "a/b", Old: "v1.0.0", New: "v1.1.0", Status: Updated, Size: 1 << 20, Dur: time.Second},
		{Repo: "c/d", New: "v2.0.0", Status: Updated, Size: 1 << 20, Dur: time.Second},
	})
	var cols []int
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "▲ a/b") || strings.Contains(l, "▲ c/d") {
			cols = append(cols, utf8.RuneCountInString(l[:strings.LastIndex(l, "v")]))
		}
	}
	if len(cols) != 2 || cols[0] != cols[1] {
		t.Fatalf("new-version column misaligned: %v\n%s", cols, out)
	}
}

func TestFailedWithoutErrorDoesNotPanic(t *testing.T) {
	out := render(Options{}, []Result{{Repo: "x/y", Status: Failed}})
	if !strings.Contains(out, "unknown error") {
		t.Fatalf("got:\n%s", out)
	}
}

func TestDryRunMarker(t *testing.T) {
	out := render(Options{}, []Result{{Repo: "a/b", Old: "v1", New: "v2", Status: Updated, DryRun: true}})
	if !strings.Contains(out, "(dry-run)") {
		t.Fatalf("got:\n%s", out)
	}
}

func TestASCIIIcons(t *testing.T) {
	out := render(Options{ASCII: true}, sample())
	for _, glyph := range []string{"✓", "▲", "✗", "⟳", "→", "─"} {
		if strings.Contains(out, glyph) {
			t.Errorf("ASCII output contains %q", glyph)
		}
	}
}

func TestCleanVersion(t *testing.T) {
	for in, want := range map[string]string{
		"ipinfo-3.3.2": "3.3.2", "range2ip-3.3.4": "3.3.4", "v18.13.1": "v18.13.1",
		"1.4.8": "1.4.8", "nightly": "nightly", "": "",
	} {
		if got := CleanVersion(in); got != want {
			t.Errorf("CleanVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWaitingAndWrap(t *testing.T) {
	var buf bytes.Buffer
	u := newUI(&buf, Options{Live: true})

	stop := u.Waiting("tool.zip", "github.com")
	time.Sleep(1100 * time.Millisecond)
	stop()
	stop() // idempotent
	if s := buf.String(); !strings.Contains(s, "waiting for server...") || !strings.HasSuffix(s, clearLine) {
		t.Fatalf("waiting line must be drawn then cleared: %q", s)
	}

	buf.Reset()
	data := strings.Repeat("x", 4096)
	r, finish := u.Wrap(strings.NewReader(data), "tool.zip", int64(len(data)))
	got, err := io.ReadAll(r)
	if err != nil || string(got) != data {
		t.Fatalf("data must pass through unchanged: %v", err)
	}
	finish(true)
	s := buf.String()
	if !strings.Contains(s, "%") || !strings.HasSuffix(s, clearLine) || strings.Contains(s, "\n") {
		t.Fatalf("progress line must be drawn and then erased without a newline: %q", s)
	}
}

func TestLiveDisabledIsSilent(t *testing.T) {
	var buf bytes.Buffer
	u := newUI(&buf, Options{Live: false})
	u.Waiting("x", "h")()
	r, finish := u.Wrap(strings.NewReader("abc"), "x", 3)
	io.ReadAll(r)
	finish(true)
	if buf.Len() != 0 {
		t.Fatalf("Live=false must not write: %q", buf.String())
	}
}

func TestDetect(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if on, _ := Detect(false, true, f); on {
		t.Error("disabled mode must stay off even when forced")
	}
	if on, _ := Detect(true, false, f); on {
		t.Error("a non-terminal must keep the classic output unless forced")
	}
	t.Setenv("NO_COLOR", "")
	on, o := Detect(true, true, f)
	if !on {
		t.Fatal("forced mode must be on for a non-terminal")
	}
	if o.Color {
		t.Error("NO_COLOR must disable colors")
	}
}

func TestDownloadedRowIsNotCountedAsUpdate(t *testing.T) {
	out := render(Options{}, []Result{
		{Repo: "cli/cli", New: "v2.68.1", Status: Downloaded, Size: 12 << 20, Dur: 3 * time.Second},
	})
	if !strings.Contains(out, "↓ cli/cli") || !strings.Contains(out, "v2.68.1") || !strings.Contains(out, "12.0 MiB") {
		t.Fatalf("unexpected row:\n%s", out)
	}
	if !strings.Contains(out, "0 updated") {
		t.Fatalf("a one-off download must not count as an update:\n%s", out)
	}
	if ascii := render(Options{ASCII: true}, []Result{{Repo: "a/b", New: "v1", Status: Downloaded}}); strings.Contains(ascii, "↓") {
		t.Errorf("ASCII output must not contain the Unicode arrow:\n%s", ascii)
	}
}

func TestNote(t *testing.T) {
	var buf bytes.Buffer
	u := newUI(&buf, Options{})
	u.Note(`d:\tools\tool.exe`)
	if got := buf.String(); !strings.Contains(got, `d:\tools\tool.exe`) || !strings.HasSuffix(got, "\n") {
		t.Fatalf("got %q", got)
	}
}
