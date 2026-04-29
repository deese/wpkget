package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/deese/wpkget/src/internal/asset"
	"github.com/deese/wpkget/src/internal/config"
	"github.com/deese/wpkget/src/internal/install"
	"github.com/deese/wpkget/src/internal/packages"
	"github.com/deese/wpkget/src/internal/zipdown"
)

// Exit codes.
const (
	exitOK       = 0
	exitGeneral  = 1
	exitNoAsset  = 2
	exitNetwork  = 3
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("wpkget: ")

	// Global flags.
	var (
		configPath string
		binDir     string
		dryRun     bool
		verbose    bool
		debug      bool
	)

	flag.StringVar(&configPath, "config", "", "path to config file (overrides WPKGET_CONFIG and default)")
	flag.StringVar(&binDir, "bin-dir", "", "destination directory (overrides config)")
	flag.BoolVar(&dryRun, "dry-run", false, "print what would be done without doing it")
	flag.BoolVar(&verbose, "verbose", false, "enable verbose output")
	flag.BoolVar(&debug, "debug", false, "print step-by-step diagnostic output")
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() == 0 {
		usage()
		os.Exit(exitGeneral)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if binDir != "" {
		cfg.BinDir = binDir
	}

	zd := zipdown.New(cfg.ZipdownURL, cfg.ZipdownToken)

	pkgListPath := pkgListFile(configPath)
	pkgList, err := packages.Load(pkgListPath)
	if err != nil {
		log.Fatalf("load package list: %v", err)
	}

	subcmd := flag.Arg(0)
	args := flag.Args()[1:]

	switch subcmd {
	case "install":
		os.Exit(handleInstall(args, cfg, pkgList, zd, dryRun, verbose, debug))
	case "update":
		os.Exit(handleUpdate(cfg, pkgList, zd, dryRun, verbose, debug))
	case "list":
		os.Exit(handleList(args, pkgList, verbose))
	case "check":
		os.Exit(handleCheck(pkgList, verbose))
	case "remove":
		os.Exit(handleRemove(args, pkgList))
	case "url":
		os.Exit(handleURL(args, verbose))
	default:
		log.Printf("unknown command %q", subcmd)
		usage()
		os.Exit(exitGeneral)
	}
}

// handleInstall downloads and installs the latest release for each given repo.
// Accepts --name to rename the installed binary (single repo only),
// --match as a glob pattern to select the release asset, and
// --all to copy all archive contents instead of a single .exe.
func handleInstall(args []string, cfg *config.Config, pkgList *packages.List, zd *zipdown.Client, dryRun, verbose, debug bool) int {
	// Parse manually so flags can appear before or after the repo argument.
	repos, binaryName, match, all, err := parseInstallArgs(args)
	if err != nil {
		log.Printf("install: %v", err)
		return exitGeneral
	}

	if len(repos) == 0 {
		log.Print("install: missing <user/repo> argument")
		return exitGeneral
	}
	if binaryName != "" && len(repos) > 1 {
		log.Print("install: --name can only be used with a single repo")
		return exitGeneral
	}

	code := exitOK
	for _, repo := range repos {
		if err := validateRepo(repo); err != nil {
			log.Printf("install %s: %v", repo, err)
			code = exitGeneral
			continue
		}

		result, err := install.Run(install.Options{
			Repo:       repo,
			BinDir:     cfg.BinDir,
			BinaryName: binaryName,
			Match:      match,
			All:        all,
			DryRun:     dryRun,
			Verbose:    verbose,
			Debug:      debug,
			Zipdown:    zd,
		})
		if err != nil {
			log.Printf("install %s: %v", repo, err)
			code = mapError(err)
			continue
		}

		if !dryRun {
			pkgList.Upsert(repo, result.Version, binaryName, match, all)
			if err := pkgList.Save(); err != nil {
				log.Printf("install %s: save package list: %v", repo, err)
				code = exitGeneral
				continue
			}
			fmt.Printf("installed %s %s → %s\n", repo, result.Version, result.BinaryPath)
		}
	}
	return code
}

// handleUpdate checks all tracked packages for new releases and installs them.
func handleUpdate(cfg *config.Config, pkgList *packages.List, zd *zipdown.Client, dryRun, verbose, debug bool) int {
	if len(pkgList.Packages) == 0 {
		fmt.Println("no packages tracked")
		return exitOK
	}

	code := exitOK
	for _, entry := range pkgList.Packages {
		result, err := install.Run(install.Options{
			Repo:       entry.Repo,
			BinDir:     cfg.BinDir,
			BinaryName: entry.BinaryName,
			Match:      entry.Match,
			All:        entry.All,
			DryRun:     dryRun,
			Verbose:    verbose,
			Debug:      debug,
			Zipdown:    zd,
		})
		if err != nil {
			log.Printf("update %s: %v", entry.Repo, err)
			code = mapError(err)
			continue
		}

		if dryRun {
			continue
		}

		if result.Version == entry.Version {
			fmt.Printf("%s is up to date (%s)\n", entry.Repo, entry.Version)
			continue
		}

		fmt.Printf("updated %s %s → %s (%s)\n", entry.Repo, entry.Version, result.Version, result.BinaryPath)
		pkgList.Upsert(entry.Repo, result.Version, entry.BinaryName, entry.Match, entry.All)
	}

	if !dryRun {
		if err := pkgList.Save(); err != nil {
			log.Printf("update: save package list: %v", err)
			return exitGeneral
		}
	}
	return code
}

// handleList prints all tracked packages.
// Accepts an optional -v flag to include the resolved download URL.
func handleList(args []string, pkgList *packages.List, verbose bool) int {
	showURL := false
	for _, a := range args {
		if a == "-v" {
			showURL = true
		}
	}

	if len(pkgList.Packages) == 0 {
		fmt.Println("no packages tracked")
		return exitOK
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if showURL {
		fmt.Fprintln(w, "REPO\tVERSION\tBINARY NAME\tURL")
	} else {
		fmt.Fprintln(w, "REPO\tVERSION\tBINARY NAME")
	}

	code := exitOK
	for _, e := range pkgList.Packages {
		if !showURL {
			fmt.Fprintf(w, "%s\t%s\t%s\n", e.Repo, e.Version, e.BinaryName)
			continue
		}
		_, url, err := install.ResolveURL(e.Repo, e.Match, verbose)
		if err != nil {
			log.Printf("list %s: resolve url: %v", e.Repo, err)
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Repo, e.Version, e.BinaryName, "(error)")
			code = mapError(err)
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Repo, e.Version, e.BinaryName, url)
	}
	w.Flush()
	return code
}

// handleCheck queries the latest release for each tracked package without downloading.
func handleCheck(pkgList *packages.List, verbose bool) int {
	if len(pkgList.Packages) == 0 {
		fmt.Println("no packages tracked")
		return exitOK
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tINSTALLED\tLATEST\tSTATUS")

	code := exitOK
	for _, e := range pkgList.Packages {
		latest, _, err := install.ResolveURL(e.Repo, e.Match, verbose)
		if err != nil {
			log.Printf("check %s: %v", e.Repo, err)
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Repo, e.Version, "(error)", "error")
			code = mapError(err)
			continue
		}
		status := "up to date"
		if latest != e.Version {
			status = "update available"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Repo, e.Version, latest, status)
	}
	w.Flush()
	return code
}

// handleRemove removes a repo from the tracking list.
func handleRemove(args []string, pkgList *packages.List) int {
	if len(args) == 0 {
		log.Print("remove: missing <user/repo> argument")
		return exitGeneral
	}

	code := exitOK
	for _, repo := range args {
		if !pkgList.Remove(repo) {
			log.Printf("remove: %s is not tracked", repo)
			code = exitGeneral
			continue
		}
		fmt.Printf("removed %s from tracking\n", repo)
	}

	if err := pkgList.Save(); err != nil {
		log.Printf("remove: save package list: %v", err)
		return exitGeneral
	}
	return code
}

// handleURL resolves and prints the download URL without downloading.
func handleURL(args []string, verbose bool) int {
	if len(args) == 0 {
		log.Print("url: missing <user/repo> argument")
		return exitGeneral
	}

	code := exitOK
	for _, repo := range args {
		if err := validateRepo(repo); err != nil {
			log.Printf("url %s: %v", repo, err)
			code = exitGeneral
			continue
		}

		version, url, err := install.ResolveURL(repo, "", verbose)
		if err != nil {
			log.Printf("url %s: %v", repo, err)
			code = mapError(err)
			continue
		}
		fmt.Printf("%s\t%s\t%s\n", repo, version, url)
	}
	return code
}

// parseInstallArgs separates repos from the --name, --match, and --all flags regardless of order.
func parseInstallArgs(args []string) (repos []string, binaryName string, match string, all bool, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--name" || arg == "-name":
			if i+1 >= len(args) {
				return nil, "", "", false, fmt.Errorf("--name requires a value")
			}
			binaryName = args[i+1]
			i++
		case strings.HasPrefix(arg, "--name="):
			binaryName = strings.TrimPrefix(arg, "--name=")
		case strings.HasPrefix(arg, "-name="):
			binaryName = strings.TrimPrefix(arg, "-name=")
		case arg == "--match" || arg == "-match":
			if i+1 >= len(args) {
				return nil, "", "", false, fmt.Errorf("--match requires a value")
			}
			match = args[i+1]
			i++
		case strings.HasPrefix(arg, "--match="):
			match = strings.TrimPrefix(arg, "--match=")
		case strings.HasPrefix(arg, "-match="):
			match = strings.TrimPrefix(arg, "-match=")
		case arg == "--all" || arg == "-all":
			all = true
		default:
			repos = append(repos, arg)
		}
	}
	return repos, binaryName, match, all, nil
}

// validateRepo checks that repo is in the "owner/name" form.
func validateRepo(repo string) error {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid repo %q: expected owner/name", repo)
	}
	return nil
}

// mapError translates domain errors to exit codes.
func mapError(err error) int {
	if errors.Is(err, asset.ErrNoAsset) {
		return exitNoAsset
	}
	return exitGeneral
}

// pkgListFile returns the path to packages.yaml, inferred from the config path.
func pkgListFile(cfgPath string) string {
	if cfgPath != "" {
		return filepath.Join(filepath.Dir(cfgPath), "packages.yaml")
	}
	appData := os.Getenv("APPDATA")
	return filepath.Join(appData, "wpkget", "packages.yaml")
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: wpkget [flags] <command> [args]

Commands:
  install <user/repo> [...]    install latest Windows release from GitHub
           [--name <binary>]   rename the installed binary (single repo only)
           [--match <pattern>] glob pattern to select the release asset
                               (e.g. --match "*windows-amd64*.zip")
           [--all]             copy all archive contents (files + folders) to bin-dir
  update                       check all tracked packages for new releases
  list    [-v]                 list tracked packages and their versions
                               -v: include resolved download URL
  check                        show latest available version for each tracked package
  remove  <user/repo> [...]    remove packages from tracking (binary not deleted)
  url     <user/repo> [...]    print the download URL without installing

Flags:
  --config  <path>   config file (default: %%APPDATA%%\wpkget\config.yaml)
  --bin-dir <path>   destination directory (overrides config)
  --dry-run          show what would be done without doing it
  --verbose          enable verbose output
  --debug            print step-by-step diagnostic output

Exit codes:
  0  success
  1  general error
  2  no suitable asset found
  3  network or GitHub API error
`)
}
