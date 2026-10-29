package config

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"unicode"
)

// Open opens path for editing. $VISUAL and $EDITOR take precedence on every
// platform; otherwise the OS default handler for the file is used. The editor
// is started directly (never through a shell) and waited for.
func Open(path string) error {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if argv := splitCommand(os.Getenv(env)); len(argv) > 0 {
			return run(append(argv, path))
		}
	}
	return run(systemOpener(path))
}

// systemOpener returns the command that opens path with the OS default handler.
func systemOpener(path string) []string {
	switch runtime.GOOS {
	case "windows":
		return []string{"rundll32", "url.dll,FileProtocolHandler", path}
	case "darwin":
		return []string{"open", path}
	default:
		return []string{"xdg-open", path}
	}
}

func run(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("config: open with %s: %w", argv[0], err)
	}
	return nil
}

// splitCommand splits a command line on whitespace, honoring double quotes
// (e.g. `"C:\Program Files\Editor\ed.exe" -w`). Backslashes are literal so
// Windows paths survive.
func splitCommand(s string) []string {
	var (
		args    []string
		cur     strings.Builder
		inQuote bool
		pending bool // an argument is in progress (covers empty quoted strings)
	)
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			pending = true
		case unicode.IsSpace(r) && !inQuote:
			if pending {
				args = append(args, cur.String())
				cur.Reset()
				pending = false
			}
		default:
			cur.WriteRune(r)
			pending = true
		}
	}
	if pending {
		args = append(args, cur.String())
	}
	return args
}
