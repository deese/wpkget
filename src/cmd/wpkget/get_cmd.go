package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/deese/wpkget/src/internal/config"
	"github.com/deese/wpkget/src/internal/install"
	"github.com/deese/wpkget/src/internal/ui"
	"github.com/deese/wpkget/src/internal/zipdown"
)

// getOptions holds the flags of the get command.
type getOptions struct {
	provider  string
	tag       string
	match     string
	name      string
	out       string
	all       bool
	noExtract bool
	force     bool
}

// runGet parses get-specific flags and delegates to handleGet.
func runGet(args []string) int {
	var (
		g    globals
		opts getOptions
	)
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	fs.Usage = usage
	registerGlobals(fs, &g)
	fs.StringVar(&opts.provider, "provider", "", "source provider: github (default) or gitlab")
	fs.StringVar(&opts.provider, "p", "", "shorthand for --provider")
	fs.StringVar(&opts.tag, "tag", "", "release tag to download (default: the latest release)")
	fs.StringVar(&opts.tag, "t", "", "shorthand for --tag")
	fs.StringVar(&opts.match, "match", "", "glob pattern to select the release asset")
	fs.StringVar(&opts.name, "name", "", "name for the extracted binary (single repo only)")
	fs.StringVar(&opts.out, "out", "", "output directory (default: the current directory)")
	fs.StringVar(&opts.out, "o", "", "shorthand for --out")
	fs.BoolVar(&opts.all, "all", false, "extract the whole archive into <repo>-<tag>/ instead of a single binary")
	fs.BoolVar(&opts.noExtract, "no-extract", false, "keep the downloaded file as is")
	fs.BoolVar(&opts.force, "force", false, "overwrite an existing destination")

	repos := parseInterspersed(fs, args)
	cfg, _, zd := setup(&g)
	return handleGet(repos, opts, cfg, zd, &g)
}

// handleGet downloads the release asset of each repo into the output directory
// without installing or tracking it.
func handleGet(repos []string, o getOptions, cfg *config.Config, zd *zipdown.Client, g *globals) int {
	if len(repos) == 0 {
		log.Print("get: missing <user/repo> argument")
		return exitGeneral
	}
	if len(repos) > 1 && (o.name != "" || o.tag != "") {
		log.Print("get: --name and --tag can only be used with a single repo")
		return exitGeneral
	}
	if o.noExtract && (o.all || o.name != "") {
		log.Print("get: --no-extract cannot be combined with --all or --name")
		return exitGeneral
	}

	var names []string
	for _, arg := range repos {
		if spec, err := parseRepoArg(arg, o.provider); err == nil {
			names = append(names, spec.repo)
		}
	}
	u := newUI(cfg, g, names, nil) // nil selects the classic output

	code := exitOK
	// fail reports an error as a row (modern output) or on stderr (classic).
	fail := func(repo string, err error, c int) {
		code = c
		if u != nil {
			u.Row(ui.Result{Repo: repo, Status: ui.Failed, Err: err})
			return
		}
		log.Printf("get %s: %v", repo, err)
	}

	for _, rawArg := range repos {
		spec, err := parseRepoArg(rawArg, o.provider)
		if err != nil {
			fail(rawArg, err, exitGeneral)
			continue
		}
		if err := validateRepo(spec.repo); err != nil {
			fail(rawArg, err, exitGeneral)
			continue
		}

		opts := install.Options{
			Repo:       spec.repo,
			Provider:   spec.provider,
			Host:       spec.host,
			Tag:        o.tag,
			BinDir:     o.out,
			BinaryName: o.name,
			Match:      o.match,
			All:        o.all,
			NoExtract:  o.noExtract,
			Force:      o.force,
			DryRun:     g.dryRun,
			Verbose:    g.verbose,
			Debug:      g.debug,
			DebugHTTP:  g.debugHTTP,
			UserAgent:  cfg.UserAgent,
			Progress:   progressOut(cfg, g),
			Zipdown:    zd,
		}
		if u != nil {
			opts.Feedback = u
		}
		result, err := install.Fetch(opts)
		if err != nil {
			fail(spec.repo, err, mapError(err))
			continue
		}

		if u != nil {
			u.Row(ui.Result{Repo: spec.repo, New: result.Version, Status: ui.Downloaded, Size: result.Size, Dur: result.Duration, DryRun: g.dryRun})
			if !g.dryRun {
				u.Note(result.BinaryPath)
			}
			continue
		}
		if !g.dryRun {
			fmt.Println(result.BinaryPath) // stdout carries only the path, so it can be used in scripts
		}
	}
	return code
}
