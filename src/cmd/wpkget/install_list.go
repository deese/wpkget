package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// installItem is one package to install: the repo argument plus the options
// that apply to it alone.
type installItem struct {
	arg        string // <user/repo> or URL, as typed
	provider   string
	binaryName string
	match      string
	all        bool
}

// loadInstallList reads an install list file; see parseInstallList.
func loadInstallList(path, defaultProvider string) ([]installItem, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseInstallList(f, defaultProvider)
}

// parseInstallList reads one package per line using the same syntax as the
// command line: the repo first, then optional --name, --match, --all and
// --provider/-p. Blank lines and everything after a '#' are ignored. Lines
// without --provider use defaultProvider.
func parseInstallList(r io.Reader, defaultProvider string) ([]installItem, error) {
	var items []installItem
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		tokens, err := splitListLine(sc.Text())
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if len(tokens) == 0 {
			continue
		}
		item := installItem{arg: tokens[0], provider: defaultProvider}
		if strings.HasPrefix(item.arg, "-") {
			return nil, fmt.Errorf("line %d: the repo must come first, got %q", n, item.arg)
		}

		fs := flag.NewFlagSet("list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		fs.StringVar(&item.binaryName, "name", "", "")
		fs.StringVar(&item.match, "match", "", "")
		fs.BoolVar(&item.all, "all", false, "")
		fs.StringVar(&item.provider, "provider", defaultProvider, "")
		fs.StringVar(&item.provider, "p", defaultProvider, "")
		if err := fs.Parse(tokens[1:]); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if fs.NArg() > 0 {
			return nil, fmt.Errorf("line %d: unexpected argument %q", n, fs.Arg(0))
		}
		items = append(items, item)
	}
	return items, sc.Err()
}

// splitListLine splits a line on whitespace, honouring double quotes, and
// stops at an unquoted '#'.
func splitListLine(line string) ([]string, error) {
	var (
		tokens   []string
		cur      strings.Builder
		inQuotes bool
		started  bool
	)
	flush := func() {
		if started {
			tokens = append(tokens, cur.String())
			cur.Reset()
			started = false
		}
	}
	for _, r := range line {
		switch {
		case r == '"':
			inQuotes = !inQuotes
			started = true
		case inQuotes:
			cur.WriteRune(r)
		case r == '#':
			flush()
			return tokens, nil
		case r == ' ' || r == '\t' || r == '\r':
			flush()
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if inQuotes {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	return tokens, nil
}
