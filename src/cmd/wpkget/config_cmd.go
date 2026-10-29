package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/deese/wpkget/src/internal/config"
	"github.com/deese/wpkget/src/internal/gitlab"
	"github.com/deese/wpkget/src/internal/packages"
)

const packagesTarget = "packages"

// runConfig parses global flags and dispatches the config sub-subcommands.
func runConfig(args []string) int {
	var g globals
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	fs.Usage = usage
	registerGlobals(fs, &g)
	pos := parseInterspersed(fs, args)

	if len(pos) == 0 {
		log.Print("config: missing subcommand (open, path, check, show)")
		return exitGeneral
	}
	sub, rest := pos[0], pos[1:]

	switch sub {
	case "open", "path":
		target, err := parseConfigTarget(sub, rest)
		if err != nil {
			log.Print(err)
			return exitGeneral
		}
		path := configTargetPath(target, &g)
		if path == "" {
			log.Printf("config %s: cannot determine the %s file location", sub, target)
			return exitGeneral
		}
		if sub == "path" {
			fmt.Println(path)
			return exitOK
		}
		return handleConfigOpen(target, path)
	case "check", "show":
		if len(rest) > 0 {
			log.Printf("config %s: unexpected argument %q", sub, rest[0])
			return exitGeneral
		}
		if sub == "check" {
			return handleConfigCheck(&g)
		}
		return handleConfigShow(&g)
	default:
		log.Printf("config: unknown subcommand %q (open, path, check, show)", sub)
		return exitGeneral
	}
}

// parseConfigTarget returns "config" (default) or "packages" for open/path.
func parseConfigTarget(sub string, rest []string) (string, error) {
	switch {
	case len(rest) == 0:
		return "config", nil
	case len(rest) == 1 && rest[0] == packagesTarget:
		return packagesTarget, nil
	default:
		return "", fmt.Errorf("config %s: expected no argument or %q, got %q", sub, packagesTarget, rest[0])
	}
}

// configTargetPath returns the file behind target ("config" or "packages").
func configTargetPath(target string, g *globals) string {
	if target == packagesTarget {
		return pkgListFile(g.configPath)
	}
	return config.Path(g.configPath)
}

// handleConfigOpen creates the file with defaults if needed and opens it for editing.
func handleConfigOpen(target, path string) int {
	var err error
	if target == packagesTarget {
		err = packages.EnsureFile(path)
	} else {
		err = config.Ensure(path)
	}
	if err != nil {
		log.Printf("config open: %v", err)
		return exitGeneral
	}
	if err := config.Open(path); err != nil {
		log.Printf("config open: %v", err)
		return exitGeneral
	}
	return exitOK
}

// handleConfigCheck validates the config syntax strictly and each GitLab host
// (CA bundle, token lookup) without contacting any server.
func handleConfigCheck(g *globals) int {
	path := config.Path(g.configPath)
	if path == "" {
		log.Print("config check: cannot determine the config file location")
		return exitGeneral
	}
	fmt.Printf("config: %s\n", path)

	if err := config.Validate(path); err != nil {
		if os.IsNotExist(err) {
			fmt.Println("file does not exist, defaults in use")
			return exitOK
		}
		fmt.Printf("invalid: %v\n", err)
		return exitGeneral
	}
	fmt.Println("syntax: ok")

	cfg, err := config.Load(g.configPath)
	if err != nil {
		fmt.Printf("invalid: %v\n", err)
		return exitGeneral
	}

	code := exitOK
	for _, st := range gitlab.Check(cfg) {
		switch {
		case st.Err != nil:
			fmt.Printf("gitlab %s: ERROR: %v\n", st.Host, st.Err)
			code = exitGeneral
		case st.HasToken:
			fmt.Printf("gitlab %s: ok (token found)\n", st.Host)
		default:
			fmt.Printf("gitlab %s: ok (no token, public projects only)\n", st.Host)
		}
	}
	return code
}

// handleConfigShow prints the config as loaded (file values over defaults)
// with secrets masked.
func handleConfigShow(g *globals) int {
	cfg, err := config.Load(g.configPath)
	if err != nil {
		log.Printf("config show: %v", err)
		return exitGeneral
	}
	out, err := cfg.RedactedYAML()
	if err != nil {
		log.Printf("config show: %v", err)
		return exitGeneral
	}
	fmt.Printf("# %s\n%s", config.Path(g.configPath), out)
	return exitOK
}
