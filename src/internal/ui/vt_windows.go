//go:build windows

package ui

import (
	"os"
	"syscall"
	"unsafe"
)

// enableVirtualTerminalProcessing is ENABLE_VIRTUAL_TERMINAL_PROCESSING.
const enableVirtualTerminalProcessing = 0x0004

// enableVT turns on ANSI escape handling for the console behind f, using only
// the standard library (no golang.org/x/sys).
func enableVT(f *os.File) bool {
	k := syscall.NewLazyDLL("kernel32.dll")
	get, set := k.NewProc("GetConsoleMode"), k.NewProc("SetConsoleMode")
	h := f.Fd()
	var mode uint32
	if r, _, _ := get.Call(h, uintptr(unsafe.Pointer(&mode))); r == 0 {
		return false
	}
	r, _, _ := set.Call(h, uintptr(mode|enableVirtualTerminalProcessing))
	return r != 0
}
