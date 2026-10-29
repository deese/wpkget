package install

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestDownloadURLProgress(t *testing.T) {
	payload := strings.Repeat("0123456789", 20000)

	newServer := func(t *testing.T, withLength bool) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if withLength {
				w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			}
			w.Write([]byte(payload))
		}))
		t.Cleanup(srv.Close)
		return srv
	}

	download := func(t *testing.T, srv *httptest.Server, bar *bytes.Buffer) {
		t.Helper()
		var err error
		var path string
		if bar != nil {
			path, err = downloadURL(srv.URL, t.TempDir(), "tool.zip", "", false, bar)
		} else {
			path, err = downloadURL(srv.URL, t.TempDir(), "tool.zip", "", false, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(path); string(got) != payload {
			t.Fatal("downloaded content differs from the payload")
		}
	}

	t.Run("known size draws a bar", func(t *testing.T) {
		var bar bytes.Buffer
		download(t, newServer(t, true), &bar)
		out := bar.String()
		if !strings.Contains(out, "tool.zip") || !strings.Contains(out, "100%") || !strings.HasSuffix(out, "\n") {
			t.Fatalf("unexpected progress output: %q", out)
		}
	})

	t.Run("unknown size shows counters only", func(t *testing.T) {
		var bar bytes.Buffer
		download(t, newServer(t, false), &bar)
		out := bar.String()
		if strings.Contains(out, "[") || !strings.Contains(out, "KiB") || !strings.HasSuffix(out, "\n") {
			t.Fatalf("unexpected progress output: %q", out)
		}
	})

	t.Run("silent when disabled", func(t *testing.T) {
		download(t, newServer(t, true), nil)
	})
}
