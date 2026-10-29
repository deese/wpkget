package progress

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

const waitInterval = time.Second

// StartWait prints "<label>  waiting for server... Ns" on w and refreshes it
// every second until the returned stop function is called. It covers the time
// before the first response byte, which can be long behind proxies that
// buffer the whole file. stop clears the line and is safe to call twice.
func StartWait(w io.Writer, label string) (stop func()) {
	start := time.Now()
	done := make(chan struct{})
	var wg sync.WaitGroup
	lastLen := 0

	draw := func() {
		line := fmt.Sprintf("%s  waiting for server... %s", fit(label, labelWidth), formatDuration(time.Since(start).Truncate(time.Second)))
		fmt.Fprintf(w, "\r%s", line)
		lastLen = len(line)
	}
	draw()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(waitInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				draw()
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			wg.Wait()
			fmt.Fprintf(w, "\r%s\r", strings.Repeat(" ", lastLen))
		})
	}
}
