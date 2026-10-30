package ui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/deese/wpkget/src/internal/progress"
)

const (
	waitInterval = time.Second
	drawInterval = 50 * time.Millisecond
)

// Waiting shows "<name> waiting for server... Ns" until the returned function
// is called. It covers the time before the first response byte, which can be
// long behind a proxy that scans the whole file first. host is unused here but
// keeps the signature of install.Feedback.
func (u *UI) Waiting(name, _ string) (stop func()) {
	if !u.o.Live {
		return func() {}
	}
	start := time.Now()
	done := make(chan struct{})
	var wg sync.WaitGroup

	draw := func() {
		fmt.Fprintf(u.w, "%s %s %s  %s", clearLine, u.yellow(u.ic.spin), progress.Fit(name, liveNameWidth),
			u.dim("waiting for server... "+progress.FormatDuration(time.Since(start).Truncate(time.Second))))
	}
	draw()

	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(waitInterval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				draw()
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			wg.Wait()
			fmt.Fprint(u.w, clearLine)
		})
	}
}

// Wrap draws a progress line while r is consumed. The returned function must
// be called once the transfer ends; it erases the line so the definitive row
// can be printed in its place. Without Live (or on a missing total) the data
// passes through untouched or with counters only.
func (u *UI) Wrap(r io.Reader, name string, total int64) (io.Reader, func(ok bool)) {
	if !u.o.Live {
		return r, func(bool) {}
	}
	lr := &liveReader{u: u, r: r, name: name, total: total, start: time.Now()}
	return lr, func(bool) {
		if lr.drawn {
			fmt.Fprint(u.w, clearLine)
		}
	}
}

type liveReader struct {
	u     *UI
	r     io.Reader
	name  string
	total int64
	done  int64
	start time.Time
	last  time.Time
	drawn bool
}

func (l *liveReader) Read(p []byte) (int, error) {
	n, err := l.r.Read(p)
	l.done += int64(n)
	if now := time.Now(); now.Sub(l.last) >= drawInterval {
		l.last = now
		l.draw()
	}
	return n, err
}

func (l *liveReader) draw() {
	u := l.u
	name := progress.Fit(l.name, liveNameWidth)
	if l.total <= 0 {
		fmt.Fprintf(u.w, "%s %s %s  %s", clearLine, u.yellow(u.ic.spin), name, u.dim(progress.FormatBytes(l.done)))
	} else {
		pct := int(l.done * 100 / l.total)
		if pct > 100 {
			pct = 100
		}
		filled := pct * barWidth / 100
		bar := u.green(strings.Repeat(u.ic.full, filled)) + u.dim(strings.Repeat(u.ic.empty, barWidth-filled))
		fmt.Fprintf(u.w, "%s %s %s %s %3d%%  %s", clearLine, u.yellow(u.ic.spin), name, bar, pct,
			u.dim(progress.FormatPair(l.done, l.total)))
	}
	l.drawn = true
}
