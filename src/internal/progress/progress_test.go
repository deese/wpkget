package progress

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func TestRender(t *testing.T) {
	cases := []struct {
		name           string
		done, total    int64
		elapsed        time.Duration
		final          bool
		wantContains   []string
		wantNotContain []string
	}{
		{"known size", 2 << 20, 4 << 20, 2 * time.Second, false,
			[]string{"[##########----------]", " 50%", "2.0/4.0 MiB", "1.0 MiB/s"}, nil},
		{"final known", 4 << 20, 4 << 20, 3 * time.Second, true,
			[]string{"[####################]", "100%", "4.0 MiB", "3.0s"}, []string{"/s"}},
		{"unknown size", 1536, 0, time.Second, false,
			[]string{"1.5 KiB", "1.5 KiB/s"}, []string{"[", "%"}},
		{"final unknown", 10, -1, 2 * time.Second, true,
			[]string{"10 B", "2.0s"}, []string{"[", "/s"}},
		{"overrun clamps", 200, 100, time.Second, false,
			[]string{"100%"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Render("asset.zip", tc.done, tc.total, tc.elapsed, tc.final)
			for _, s := range tc.wantContains {
				if !strings.Contains(got, s) {
					t.Errorf("%q missing %q", got, s)
				}
			}
			for _, s := range tc.wantNotContain {
				if strings.Contains(got, s) {
					t.Errorf("%q must not contain %q", got, s)
				}
			}
		})
	}
}

func TestRenderWidthIsStable(t *testing.T) {
	a := Render("a.zip", 0, 100, time.Second, false)
	b := Render("a-very-long-asset-name-windows-amd64.zip", 0, 100, time.Second, false)
	if len(a) != len(b) {
		t.Fatalf("label must not change the line width: %d vs %d", len(a), len(b))
	}
}

func TestFit(t *testing.T) {
	if got := fit("ab", 5); got != "ab   " {
		t.Errorf("pad: %q", got)
	}
	got := fit("0123456789abcdefghijklmnopqrstuvwxyz", labelWidth)
	if len([]rune(got)) != labelWidth || !strings.Contains(got, "...") {
		t.Errorf("shorten: %q", got)
	}
	if !strings.HasPrefix(got, "0123456789") || !strings.HasSuffix(got, "stuvwxyz") {
		t.Errorf("must keep head and tail: %q", got)
	}
}

func TestFormatBytes(t *testing.T) {
	cases := map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 5 << 20: "5.0 MiB", 3 << 30: "3.0 GiB"}
	for in, want := range cases {
		if got := formatBytes(in); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
	if got := formatDuration(65 * time.Second); got != "1m05s" {
		t.Errorf("formatDuration = %q", got)
	}
}

func TestReaderCopiesAndFinishes(t *testing.T) {
	data := strings.Repeat("x", 4096)
	var out bytes.Buffer
	clock := time.Unix(0, 0)
	pr := NewReader(strings.NewReader(data), &out, "f.zip", int64(len(data)))
	pr.now = func() time.Time { clock = clock.Add(200 * time.Millisecond); return clock }
	pr.start = clock

	var sink bytes.Buffer
	if _, err := io.Copy(&sink, pr); err != nil {
		t.Fatal(err)
	}
	pr.Finish()
	pr.Close() // no-op after Finish

	if sink.String() != data {
		t.Fatal("wrapped reader must pass data through unchanged")
	}
	s := out.String()
	if !strings.HasPrefix(s, "\r") || !strings.HasSuffix(s, "\n") || strings.Count(s, "\n") != 1 {
		t.Fatalf("expected carriage-return updates and one final newline, got %q", s)
	}
	if !strings.Contains(s, "100%") {
		t.Fatalf("final line must show 100%%: %q", s)
	}
}

func TestCloseTerminatesAbortedLine(t *testing.T) {
	var out bytes.Buffer
	pr := NewReader(strings.NewReader("abc"), &out, "f", 100)
	pr.now = func() time.Time { return time.Unix(10, 0) }
	buf := make([]byte, 3)
	pr.Read(buf)
	pr.Close()
	if !strings.HasSuffix(out.String(), "\n") {
		t.Fatalf("aborted bar must end with a newline, got %q", out.String())
	}

	var quiet bytes.Buffer
	NewReader(strings.NewReader(""), &quiet, "f", 1).Close()
	if quiet.Len() != 0 {
		t.Fatal("Close must not print when nothing was drawn")
	}
}

func TestValidModeAndEnabled(t *testing.T) {
	for _, m := range []string{Auto, Always, Never} {
		if !ValidMode(m) {
			t.Errorf("%q must be valid", m)
		}
	}
	if ValidMode("sometimes") || ValidMode("") {
		t.Error("unexpected valid mode")
	}
	if !Enabled(Always, nil) || Enabled(Never, nil) {
		t.Error("always/never must not depend on the file")
	}
}
