package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/deese/wpkget/src/internal/asset"
	"github.com/deese/wpkget/src/internal/config"
	"github.com/deese/wpkget/src/internal/github"
	"github.com/deese/wpkget/src/internal/gitlab"
	"github.com/deese/wpkget/src/internal/install"
	"github.com/deese/wpkget/src/internal/packages"
	"github.com/deese/wpkget/src/internal/zipdown"
)

// version is overridden at build time via -ldflags "-X main.version=vX.Y".
var version = "v1.3.7"

// Exit codes.
const (
	exitOK      = 0
	exitGeneral = 1
	exitNoAsset = 2
	exitNetwork = 3
)

// globals holds flag values that apply to every subcommand.
type globals struct {
	configPath string
	binDir     string
	userAgent  string
	dryRun     bool
	verbose    bool
	debug      bool
	debugHTTP  bool
}

// registerGlobals binds global flags to g on the given FlagSet.
func registerGlobals(fs *flag.FlagSet, g *globals) {
	fs.StringVar(&g.configPath, "config", "", "path to config file (overrides WPKGET_CONFIG and default)")
	fs.StringVar(&g.binDir, "bin-dir", "", "destination directory (overrides config)")
	fs.StringVar(&g.userAgent, "user-agent", "", "User-Agent header for HTTP requests (overrides config)")
	fs.BoolVar(&g.dryRun, "dry-run", false, "print what would be done without doing it")
	fs.BoolVar(&g.verbose, "verbose", false, "enable verbose output")
	fs.BoolVar(&g.debug, "debug", false, "print step-by-step diagnostic output")
	fs.BoolVar(&g.debugHTTP, "debug-http", false, "dump outgoing HTTP requests and response bodies")
}

// parseInterspersed calls fs.Parse repeatedly, collecting positional arguments
// that appear between flags. This allows flags and positionals in any order.
func parseInterspersed(fs *flag.FlagSet, args []string) []string {
	var positionals []string
	for len(args) > 0 {
		_ = fs.Parse(args) // stops at first non-flag token
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		positionals = append(positionals, rest[0])
		args = rest[1:]
	}
	return positionals
}

// setup loads config, the package list, and the zipdown client from g.
func setup(g *globals) (*config.Config, *packages.List, *zipdown.Client) {
	cfg, err := config.Load(g.configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if g.binDir != "" {
		cfg.BinDir = g.binDir
	}
	if g.userAgent != "" {
		cfg.UserAgent = g.userAgent
	}
	gitlab.Configure(cfg)
	zd := zipdown.New(cfg.ZipdownURL, cfg.ZipdownToken, cfg.UserAgent)
	zd.SetDebugHTTP(g.debugHTTP)
	pkgList, err := packages.Load(pkgListFile(g.configPath))
	if err != nil {
		log.Fatalf("load package list: %v", err)
	}
	return cfg, pkgList, zd
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("wpkget: ")

	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(exitGeneral)
	}
	if args[0] == "--version" || args[0] == "-version" {
		fmt.Println(version)
		os.Exit(exitOK)
	}

	// Detect the subcommand: parse a scratch FlagSet (global flags only) to
	// skip any global flags that precede the subcommand token.
	scratch := flag.NewFlagSet("", flag.ContinueOnError)
	scratch.SetOutput(io.Discard)
	var scratchG globals
	registerGlobals(scratch, &scratchG)
	scratch.Parse(args) // stops at first positional (the subcommand)

	if scratch.NArg() == 0 {
		log.Print("no subcommand given")
		usage()
		os.Exit(exitGeneral)
	}

	subcmd := scratch.Arg(0)

	// Reconstruct the arg list without the subcommand token.
	// Flags that appeared before the subcommand are preserved so the
	// per-subcommand FlagSet can re-parse them together with everything else.
	nFlagsBeforeSubcmd := len(args) - scratch.NArg()
	rest := append(
		args[:nFlagsBeforeSubcmd:nFlagsBeforeSubcmd],
		scratch.Args()[1:]...,
	)

	switch subcmd {
	case "install":
		os.Exit(runInstall(rest))
	case "update":
		os.Exit(runUpdate(rest))
	case "check":
		os.Exit(runCheck(rest))
	case "list":
		os.Exit(runList(rest))
	case "remove":
		os.Exit(runRemove(rest))
	case "url":
		os.Exit(runURL(rest))
	case "show":
		os.Exit(runShow(rest))
	case "version", "--version", "-version":
		fmt.Println(version)
		os.Exit(exitOK)
	default:
		log.Printf("unknown command %q", subcmd)
		usage()
		os.Exit(exitGeneral)
	}
}

// runInstall parses install-specific flags and delegates to handleInstall.
func runInstall(args []string) int {
	var (
		g          globals
		binaryName string
		match      string
		all        bool
		provider   string
	)
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	fs.Usage = usage
	registerGlobals(fs, &g)
	fs.StringVar(&binaryName, "name", "", "rename the installed binary (single repo only)")
	fs.StringVar(&match, "match", "", "glob pattern to select the release asset")
	fs.BoolVar(&all, "all", false, "copy all archive contents to bin-dir")
	fs.StringVar(&provider, "provider", "", "source provider: github (default) or gitlab")
	fs.StringVar(&provider, "p", "", "shorthand for --provider")

	repos := parseInterspersed(fs, args)
	cfg, pkgList, zd := setup(&g)
	return handleInstall(repos, binaryName, g.binDir, match, all, provider, cfg, pkgList, zd, &g)
}

// runUpdate parses global flags and delegates to handleUpdate.
func runUpdate(args []string) int {
	var g globals
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	fs.Usage = usage
	registerGlobals(fs, &g)
	parseInterspersed(fs, args)
	cfg, pkgList, zd := setup(&g)
	return handleUpdate(cfg, pkgList, zd, &g)
}

// runCheck parses global flags and delegates to handleCheck.
func runCheck(args []string) int {
	var g globals
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	fs.Usage = usage
	registerGlobals(fs, &g)
	parseInterspersed(fs, args)
	cfg, pkgList, _ := setup(&g)
	return handleCheck(pkgList, cfg, &g)
}

// runList parses the -v flag plus globals and delegates to handleList.
func runList(args []string) int {
	var (
		g       globals
		showURL bool
	)
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	fs.Usage = usage
	registerGlobals(fs, &g)
	fs.BoolVar(&showURL, "v", false, "include resolved download URL")
	parseInterspersed(fs, args)
	cfg, pkgList, _ := setup(&g)
	return handleList(showURL, pkgList, cfg, &g)
}

// runRemove parses global flags and delegates to handleRemove.
func runRemove(args []string) int {
	var g globals
	fs := flag.NewFlagSet("remove", flag.ExitOnError)
	fs.Usage = usage
	registerGlobals(fs, &g)
	repos := parseInterspersed(fs, args)
	_, pkgList, _ := setup(&g)
	return handleRemove(repos, pkgList)
}

// runURL parses global flags and delegates to handleURL.
func runURL(args []string) int {
	var (
		g        globals
		provider string
	)
	fs := flag.NewFlagSet("url", flag.ExitOnError)
	fs.Usage = usage
	registerGlobals(fs, &g)
	fs.StringVar(&provider, "provider", "", "source provider: github (default) or gitlab")
	fs.StringVar(&provider, "p", "", "shorthand for --provider")
	repos := parseInterspersed(fs, args)
	cfg, _, _ := setup(&g)
	return handleURL(repos, provider, cfg, &g)
}

// runShow parses global flags and delegates to handleShow.
func runShow(args []string) int {
	var (
		g        globals
		provider string
	)
	fs := flag.NewFlagSet("show", flag.ExitOnError)
	fs.Usage = usage
	registerGlobals(fs, &g)
	fs.StringVar(&provider, "provider", "", "source provider: github (default) or gitlab")
	fs.StringVar(&provider, "p", "", "shorthand for --provider")
	repos := parseInterspersed(fs, args)
	cfg, pkgList, _ := setup(&g)
	return handleShow(repos, provider, pkgList, cfg, &g)
}

// handleInstall downloads and installs the latest release for each given repo.
func handleInstall(repos []string, binaryName, binDir, match string, all bool, flagProvider string, cfg *config.Config, pkgList *packages.List, zd *zipdown.Client, g *globals) int {
	if len(repos) == 0 {
		log.Print("install: missing <user/repo> argument")
		return exitGeneral
	}
	if binaryName != "" && len(repos) > 1 {
		log.Print("install: --name can only be used with a single repo")
		return exitGeneral
	}

	effectiveBinDir := cfg.BinDir
	if binDir != "" {
		effectiveBinDir = binDir
	}

	code := exitOK
	for _, rawArg := range repos {
		spec, err := parseRepoArg(rawArg, flagProvider)
		if err != nil {
			log.Printf("install %s: %v", rawArg, err)
			code = exitGeneral
			continue
		}
		if err := validateRepo(spec.repo); err != nil {
			log.Printf("install %s: %v", rawArg, err)
			code = exitGeneral
			continue
		}

		result, err := install.Run(install.Options{
			Repo:       spec.repo,
			Provider:   spec.provider,
			Host:       spec.host,
			BinDir:     effectiveBinDir,
			BinaryName: binaryName,
			Match:      match,
			All:        all,
			DryRun:     g.dryRun,
			Verbose:    g.verbose,
			Debug:      g.debug,
			DebugHTTP:  g.debugHTTP,
			UserAgent:  cfg.UserAgent,
			Zipdown:    zd,
		})
		if err != nil {
			log.Printf("install %s: %v", spec.repo, err)
			code = mapError(err)
			continue
		}

		if !g.dryRun {
			pkgList.Upsert(spec.repo, spec.provider, spec.host, result.Version, binaryName, binDir, match, all)
			if err := pkgList.Save(); err != nil {
				log.Printf("install %s: save package list: %v", spec.repo, err)
				code = exitGeneral
				continue
			}
			fmt.Printf("installed %s %s → %s\n", spec.repo, result.Version, result.BinaryPath)
		}
	}
	return code
}

// handleUpdate checks all tracked packages for new releases and installs them.
func handleUpdate(cfg *config.Config, pkgList *packages.List, zd *zipdown.Client, g *globals) int {
	if len(pkgList.Packages) == 0 {
		fmt.Println("no packages tracked")
		return exitOK
	}

	code := exitOK
	for _, entry := range pkgList.Packages {
		if entry.KeepVersion {
			fmt.Printf("%s is pinned, skipping\n", entry.Repo)
			continue
		}
		latest, _, err := install.ResolveURL(entry.Provider, entry.Host, entry.Repo, entry.Match, cfg.UserAgent, g.verbose, g.debugHTTP)
		if err != nil {
			log.Printf("update %s: %v", entry.Repo, err)
			code = mapError(err)
			continue
		}

		if latest == entry.Version {
			fmt.Printf("%s is up to date (%s)\n", entry.Repo, entry.Version)
			continue
		}

		if g.dryRun {
			fmt.Printf("dry-run: would update %s %s → %s\n", entry.Repo, entry.Version, latest)
			continue
		}

		effectiveBinDir := cfg.BinDir
		if entry.BinDir != "" {
			effectiveBinDir = entry.BinDir
		}

		result, err := install.Run(install.Options{
			Repo:       entry.Repo,
			Provider:   entry.Provider,
			Host:       entry.Host,
			BinDir:     effectiveBinDir,
			BinaryName: entry.BinaryName,
			Match:      entry.Match,
			All:        entry.All,
			Verbose:    g.verbose,
			Debug:      g.debug,
			DebugHTTP:  g.debugHTTP,
			UserAgent:  cfg.UserAgent,
			Zipdown:    zd,
		})
		if err != nil {
			log.Printf("update %s: %v", entry.Repo, err)
			code = mapError(err)
			continue
		}

		fmt.Printf("updated %s %s → %s (%s)\n", entry.Repo, entry.Version, result.Version, result.BinaryPath)
		pkgList.Upsert(entry.Repo, entry.Provider, entry.Host, result.Version, entry.BinaryName, entry.BinDir, entry.Match, entry.All)
	}

	if !g.dryRun {
		if err := pkgList.Save(); err != nil {
			log.Printf("update: save package list: %v", err)
			return exitGeneral
		}
	}
	return code
}

// handleList prints all tracked packages.
func handleList(showURL bool, pkgList *packages.List, cfg *config.Config, g *globals) int {
	if len(pkgList.Packages) == 0 {
		fmt.Println("no packages tracked")
		return exitOK
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if showURL {
		fmt.Fprintln(w, "REPO\tVERSION\tBINARY NAME\tBIN DIR\tURL")
	} else {
		fmt.Fprintln(w, "REPO\tVERSION\tBINARY NAME\tBIN DIR")
	}

	code := exitOK
	for _, e := range pkgList.Packages {
		if !showURL {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Repo, e.Version, e.BinaryName, e.BinDir)
			continue
		}
		_, url, err := install.ResolveURL(e.Provider, e.Host, e.Repo, e.Match, cfg.UserAgent, g.verbose, g.debugHTTP)
		if err != nil {
			log.Printf("list %s: resolve url: %v", e.Repo, err)
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.Repo, e.Version, e.BinaryName, e.BinDir, "(error)")
			code = mapError(err)
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.Repo, e.Version, e.BinaryName, e.BinDir, url)
	}
	w.Flush()
	return code
}

// handleCheck queries the latest release for each tracked package without downloading.
func handleCheck(pkgList *packages.List, cfg *config.Config, g *globals) int {
	if len(pkgList.Packages) == 0 {
		fmt.Println("no packages tracked")
		return exitOK
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tINSTALLED\tLATEST\tSTATUS")

	code := exitOK
	for _, e := range pkgList.Packages {
		if e.KeepVersion {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Repo, e.Version, "-", "pinned")
			continue
		}
		latest, _, err := install.ResolveURL(e.Provider, e.Host, e.Repo, e.Match, cfg.UserAgent, g.verbose, g.debugHTTP)
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

// handleRemove removes repos from the tracking list.
func handleRemove(repos []string, pkgList *packages.List) int {
	if len(repos) == 0 {
		log.Print("remove: missing <user/repo> argument")
		return exitGeneral
	}

	code := exitOK
	for _, repo := range repos {
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
func handleURL(repos []string, flagProvider string, cfg *config.Config, g *globals) int {
	if len(repos) == 0 {
		log.Print("url: missing <user/repo> argument")
		return exitGeneral
	}

	code := exitOK
	for _, rawArg := range repos {
		spec, err := parseRepoArg(rawArg, flagProvider)
		if err != nil {
			log.Printf("url %s: %v", rawArg, err)
			code = exitGeneral
			continue
		}
		if err := validateRepo(spec.repo); err != nil {
			log.Printf("url %s: %v", rawArg, err)
			code = exitGeneral
			continue
		}

		version, url, err := install.ResolveURL(spec.provider, spec.host, spec.repo, "", cfg.UserAgent, g.verbose, g.debugHTTP)
		if err != nil {
			log.Printf("url %s: %v", spec.repo, err)
			code = mapError(err)
			continue
		}
		fmt.Printf("%s\t%s\t%s\n", spec.repo, version, url)
	}
	return code
}

// handleShow prints detailed information about a package and lists all available release assets.
func handleShow(repos []string, flagProvider string, pkgList *packages.List, cfg *config.Config, g *globals) int {
	if len(repos) == 0 {
		log.Print("show: missing <user/repo> argument")
		return exitGeneral
	}

	code := exitOK
	for i, rawArg := range repos {
		spec, err := parseRepoArg(rawArg, flagProvider)
		if err != nil {
			log.Printf("show %s: %v", rawArg, err)
			code = exitGeneral
			continue
		}
		if err := validateRepo(spec.repo); err != nil {
			log.Printf("show %s: %v", rawArg, err)
			code = exitGeneral
			continue
		}

		if i > 0 {
			fmt.Println()
		}

		var release *github.Release
		if spec.provider == "gitlab" {
			release, err = gitlab.LatestRelease(spec.host, spec.repo, cfg.UserAgent, g.debugHTTP)
		} else {
			release, err = github.LatestRelease(spec.repo, cfg.UserAgent, g.debugHTTP)
		}
		if err != nil {
			log.Printf("show %s: %v", spec.repo, err)
			code = mapError(err)
			continue
		}

		fmt.Printf("repo:     %s\n", spec.repo)
		fmt.Printf("provider: %s\n", spec.provider)
		fmt.Printf("latest:   %s\n", release.TagName)

		if entry := pkgList.Get(spec.repo); entry != nil {
			fmt.Printf("tracked: %s", entry.Version)
			if entry.Version == release.TagName {
				fmt.Print(" (up to date)")
			} else {
				fmt.Print(" (update available)")
			}
			fmt.Println()
			if entry.BinaryName != "" {
				fmt.Printf("binary:  %s\n", entry.BinaryName)
			}
			if entry.BinDir != "" {
				fmt.Printf("bin_dir: %s\n", entry.BinDir)
			}
			if entry.Match != "" {
				fmt.Printf("match:   %s\n", entry.Match)
			}
			if entry.All {
				fmt.Printf("all:     true\n")
			}
			if entry.KeepVersion {
				fmt.Printf("pinned:  true\n")
			}
		} else {
			fmt.Println("tracked: no")
		}

		fmt.Printf("\nassets (%d):\n", len(release.Assets))
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		for _, a := range release.Assets {
			fmt.Fprintf(w, "  %s\t%s\n", a.Name, a.BrowserDownloadURL)
		}
		w.Flush()
	}
	return code
}

// validateRepo checks that repo is in the "owner/name" form.
func validateRepo(repo string) error {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid repo %q: expected owner/name", repo)
	}
	return nil
}

// repoSpec holds a parsed repo argument.
type repoSpec struct {
	repo     string // "owner/name" (GitLab: "group[/subgroup]/name")
	provider string // "github" or "gitlab"
	host     string // self-hosted GitLab host; empty for github.com and gitlab.com
}

// parseRepoArg accepts either:
//   - "owner/repo"                          → uses the flag-level provider (default "github")
//   - "https://github.com/owner/repo[/...]" → provider = "github"
//   - "https://gitlab.com/group/repo[/...]" → provider = "gitlab"
//   - "https://<host>/group/repo[/...]"     → provider = "gitlab", <host> must be listed in gitlab_hosts
//
// The flagProvider is the value of --provider/-p and is used as a fallback
// when the argument is not a URL.
func parseRepoArg(arg, flagProvider string) (repoSpec, error) {
	if strings.HasPrefix(arg, "https://") || strings.HasPrefix(arg, "http://") {
		u, err := url.Parse(arg)
		if err != nil {
			return repoSpec{}, fmt.Errorf("invalid URL %q: %w", arg, err)
		}
		host := strings.ToLower(u.Host)
		path := strings.Trim(u.Path, "/")

		if host == "github.com" {
			// Extract up to two path segments: /owner/repo (ignore sub-paths like /releases)
			segments := strings.SplitN(path, "/", 3)
			if len(segments) < 2 || segments[0] == "" || segments[1] == "" {
				return repoSpec{}, fmt.Errorf("cannot extract owner/repo from URL %q", arg)
			}
			return repoSpec{repo: segments[0] + "/" + segments[1], provider: "github"}, nil
		}

		if !gitlab.IsKnownHost(host) {
			return repoSpec{}, fmt.Errorf("unsupported host %q in URL %q (add it to gitlab_hosts in the config)", u.Host, arg)
		}
		// GitLab project paths may include subgroups; GitLab UI sub-paths start at "/-/".
		path = gitlab.TrimPrefix(host, path)
		path, _, _ = strings.Cut(path, "/-/")
		path = strings.TrimSuffix(path, ".git")
		if !strings.Contains(path, "/") {
			return repoSpec{}, fmt.Errorf("cannot extract group/project from URL %q", arg)
		}
		spec := repoSpec{repo: path, provider: "gitlab"}
		if host != gitlab.DefaultHost {
			spec.host = host
		}
		return spec, nil
	}
	// Plain "owner/repo" — use the flag-level provider.
	provider := flagProvider
	if provider == "" {
		provider = "github"
	}
	return repoSpec{repo: arg, provider: provider}, nil
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
	return filepath.Join(config.DefaultDir(), "packages.yaml")
}

func usage() {
	fmt.Fprintf(os.Stderr, "wpkget %s\n", version)
	fmt.Fprint(os.Stderr, `
Usage: wpkget [flags] <command> [args]

Commands:
  install <repo> [...]         install latest release (GitHub by default)
           [--provider/-p <p>] source provider: github (default) or gitlab
           [--name <binary>]   rename the installed binary (single repo only)
           [--match <pattern>] glob pattern to select the release asset
                               (e.g. --match "*windows-amd64*.zip")
           [--all]             copy all archive contents (files + folders) to bin-dir
           [--bin-dir <path>]  destination directory for this package (saved per-package)
  update                       check all tracked packages for new releases
  list    [-v]                 list tracked packages and their versions
                               -v: include resolved download URL
  check                        show latest available version for each tracked package
  remove  <user/repo> [...]    remove packages from tracking (binary not deleted)
  url     <user/repo> [...]    print the download URL without installing
           [--provider/-p <p>] source provider: github (default) or gitlab
  show    <user/repo> [...]    show package details and all available release assets
           [--provider/-p <p>] source provider: github (default) or gitlab
  version                      print version and exit

Repo argument formats:
  owner/repo                          GitHub (default)
  https://github.com/owner/repo       GitHub (explicit URL)
  https://gitlab.com/owner/repo[/...] GitLab (auto-detected from URL)
  owner/repo --provider gitlab        GitLab (explicit flag)

Flags (accepted anywhere on the command line):
  --config    <path>   config file (default: %APPDATA%\wpkget\config.yaml)
  --bin-dir   <path>   destination directory (overrides config)
  --user-agent <ua>    User-Agent header for HTTP requests (overrides config)
  --dry-run            show what would be done without installing
  --verbose            enable verbose output
  --debug              print step-by-step diagnostic output
  --debug-http         dump outgoing HTTP requests and response bodies

Exit codes:
  0  success
  1  general error
  2  no suitable asset found
  3  network or API error
`)
}
