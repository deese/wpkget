package progress

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestStartWait(t *testing.T) {
	var out bytes.Buffer
	stop := StartWait(&out, "tool.zip")
	time.Sleep(1200 * time.Millisecond) // long enough for one refresh
	stop()
	stop() // idempotent

	s := out.String()
	if !strings.Contains(s, "waiting for server... 0.0s") && !strings.Contains(s, "waiting for server... 0s") {
		t.Fatalf("initial line missing: %q", s)
	}
	if !strings.Contains(s, "waiting for server... 1.0s") {
		t.Fatalf("counter must refresh: %q", s)
	}
	if !strings.HasSuffix(s, "\r") || strings.Contains(s, "\n") {
		t.Fatalf("stop must clear the line without a newline: %q", s)
	}
}
