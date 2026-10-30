package progress

import "time"

// Exported formatting helpers, shared with the modern terminal output so both
// renderers print sizes, durations and shortened names the same way.

// Fit pads or shortens s to exactly width runes, cutting from the middle.
func Fit(s string, width int) string { return fit(s, width) }

// FormatBytes renders a size such as "2.1 MiB".
func FormatBytes(n int64) string { return formatBytes(n) }

// FormatPair renders "done/total UNIT", e.g. "2.1/4.7 MiB".
func FormatPair(done, total int64) string { return formatPair(done, total) }

// FormatDuration renders a duration such as "2.4s" or "1m05s".
func FormatDuration(d time.Duration) string { return formatDuration(d) }
