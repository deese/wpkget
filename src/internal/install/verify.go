package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/deese/wpkget/src/internal/github"
)

// Download timeouts. They are variables so tests can shorten them.
//
// There is deliberately no overall timeout: a large file over a slow link, or
// behind a proxy that buffers the whole file before forwarding it, can take
// minutes while still making progress.
var (
	// downloadHeaderTimeout bounds the wait for the response headers.
	downloadHeaderTimeout = 10 * time.Minute
	// downloadIdleTimeout bounds the time without receiving any body data.
	downloadIdleTimeout = 2 * time.Minute
)

// idleReader resets timer every time data is read, so the request context is
// only cancelled when the transfer stalls.
type idleReader struct {
	r     io.Reader
	timer *time.Timer
}

func (i *idleReader) Read(p []byte) (int, error) {
	n, err := i.r.Read(p)
	if n > 0 {
		i.timer.Reset(downloadIdleTimeout)
	}
	return n, err
}

// newDownloadContext returns a context cancelled after downloadHeaderTimeout
// unless the timer is reset by the caller once the headers arrive.
func newDownloadContext() (context.Context, context.CancelFunc, *time.Timer) {
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(downloadHeaderTimeout, cancel)
	return ctx, func() { timer.Stop(); cancel() }, timer
}

// verifyDownload checks the file at path against the size and SHA-256 digest
// published for the asset. Checks are skipped for values the provider did not
// report.
func verifyDownload(path string, a *github.Asset) error {
	if a.Size > 0 {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("install: verify download: %w", err)
		}
		if info.Size() != a.Size {
			return fmt.Errorf("install: size mismatch for %s: got %d bytes, expected %d (the download was truncated or modified in transit)", a.Name, info.Size(), a.Size)
		}
	}

	want, ok := strings.CutPrefix(strings.ToLower(a.Digest), "sha256:")
	if !ok || want == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("install: verify download: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("install: verify download: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("install: sha256 mismatch for %s: got %s, expected %s (the download was modified in transit)", a.Name, got, want)
	}
	return nil
}
