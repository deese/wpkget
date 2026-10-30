package install

import (
	"fmt"
	"io"

	"github.com/deese/wpkget/src/internal/progress"
)

// Feedback receives download events so the caller decides how to present them.
// A nil Feedback means a silent download. Both the classic output (legacyFeedback)
// and the modern one (internal/ui) implement it.
type Feedback interface {
	// Waiting is called before the request is sent. The returned function is
	// called once the response headers arrive, or the request fails.
	Waiting(name, host string) (stop func())
	// Wrap wraps the response body. The returned function is called when the
	// transfer ends, with ok=false if it was aborted.
	Wrap(r io.Reader, name string, total int64) (wrapped io.Reader, finish func(ok bool))
}

// legacyFeedback is the classic output: a "downloading" notice, a live wait
// counter and a progress bar that stays as a summary line.
type legacyFeedback struct{ w io.Writer }

func (l legacyFeedback) Waiting(name, host string) func() {
	fmt.Fprintf(l.w, "downloading %s from %s\n", name, host)
	return progress.StartWait(l.w, name)
}

func (l legacyFeedback) Wrap(r io.Reader, name string, total int64) (io.Reader, func(bool)) {
	bar := progress.NewReader(r, l.w, name, total)
	return bar, func(ok bool) {
		if ok {
			bar.Finish()
			return
		}
		bar.Close()
	}
}
