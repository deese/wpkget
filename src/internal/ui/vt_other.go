//go:build !windows

package ui

import "os"

// enableVT is a no-op: terminals outside Windows handle ANSI escapes natively.
func enableVT(*os.File) bool { return true }
