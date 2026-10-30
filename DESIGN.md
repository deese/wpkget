# wpkget — Design Document

## Purpose

wpkget is a single-binary Windows CLI tool that automates the download and installation of compiled binaries distributed as GitHub or GitLab release assets (gitlab.com and self-hosted GitLab). It is intentionally narrow in scope: it does not manage PATH, does not handle version pinning beyond "track latest", and does not install MSI/NSIS installers.

---

## Repository layout

```
wpkget/
├── src/
│   ├── cmd/
│   │   └── wpkget/
│   │       └── main.go          # entry point, CLI wiring
│   └── internal/
│       ├── github/
│       │   └── github.go        # GitHub Releases API client
│       ├── gitlab/
│       │   └── gitlab.go        # GitLab Releases API client, per-host token/TLS
│       ├── asset/
│       │   └── asset.go         # asset selection heuristics
│       ├── install/
│       │   └── install.go       # download → decompress → move → cleanup
│       ├── packages/
│       │   └── packages.go      # local package list (read/write YAML)
│       ├── config/
│       │   └── config.go        # config file loading and defaults
│       ├── progress/
│       │   └── progress.go      # stdlib-only download progress bar (classic output)
│       ├── ui/
│       │   └── ui.go, live.go   # opt-in modern output (colors, rows, live line, summary)
│       └── zipdown/
│           └── zipdown.go       # zipdown service client
└── go.mod
```

All packages under `internal/` are unexported to keep the public surface minimal and allow free refactoring.

---

## CLI design

The entry point uses the standard `flag` package with subcommand dispatch. No third-party CLI framework is used — the command set is small enough that the standard library suffices and avoids dependency bloat.

Subcommands: `install`, `update`, `check`, `list`, `remove`, `url`, `show`, `config`.

`config` has its own sub-subcommands (`open`, `path`, `check`, `show`; see [Configuration](#configuration)), implemented in `cmd/wpkget/config_cmd.go`.

`install`, `url` and `show` accept `--provider github|gitlab` (default `github`). A repo argument may also be a full URL, in which case the provider (and, for GitLab, the host) is derived from it; see [GitLab](#gitlab).

Global flags (`--config`, `--bin-dir`, `--user-agent`, `--dry-run`, `--verbose`, `--debug`, `--debug-http`) are parsed before the subcommand is dispatched.

---

## GitHub Releases API

Endpoint used:

```
GET https://api.github.com/repos/{owner}/{repo}/releases/latest
```

The response is decoded into a minimal struct containing only the fields we need: `tag_name` and the `assets` array (each asset has `name` and `browser_download_url`).

**Authentication:** The GitHub API allows 60 unauthenticated requests per hour per IP. wpkget does not require a token but will read one from the `GITHUB_TOKEN` environment variable and attach it as a `Bearer` header when present. This raises the limit to 5 000 requests/hour and avoids rate-limit errors in CI or heavy-use scenarios.

**HTTP client:** `net/http` default client with a 30-second timeout. No retries — a failed network call surfaces as a clear error; the user can re-run.

---

## GitLab

`internal/gitlab` exposes the same `github.Release` shape as the GitHub client, so asset selection and the install pipeline are provider-agnostic. The provider is chosen in `install.latestRelease`.

Endpoint used (the first item is the newest release):

```
GET https://{host}{prefix}/api/v4/projects/{url-escaped group/subgroup/project}/releases?per_page=1
```

Assets are the release's `assets.links` (name + URL). Source archives are ignored. Nested subgroups are supported; the repo base name is the last path segment.

**Host allow-list.** Hosts are never inferred from user input. A URL argument is accepted only for `github.com`, `gitlab.com`, or a host listed in `gitlab_hosts`; anything else is rejected. This is the control that prevents sending a token to an arbitrary server. A self-hosted entry is stored in `packages.yaml` as `host`, so `update`/`check`/`list` return to the same instance (`host` is omitted for gitlab.com).

**Per-host settings** (`gitlab_hosts.<host>` in the config):

| Field | Description |
|-------|-------------|
| `prefix` | URL path under which GitLab is served (e.g. `/gitlab`). Stripped from URL arguments and prepended to the API URL. The key stays the bare host, so two instances on one host with different prefixes are not supported. |
| `token_env` | Environment variable holding the token. Checked first. |
| `token_pass` | pass-compatible entry. Runs `<pass_cmd> show <entry>` (no shell; stdin/stderr inherited for passphrase prompts) and uses the first line. `pass_cmd` defaults to `winpass` on Windows and `pass` elsewhere. Entries starting with `-` are rejected. |
| `ca_file` | PEM bundle added to the system roots. `$VAR`/`${VAR}` are expanded; when empty, `WPKGET_CA_FILE` is used. |
| `skip_verify` | Disables certificate verification for that host and logs a warning. Explicit opt-in; `ca_file` is the recommended option. |

**Token handling.** Sessions (HTTP client + token) are resolved lazily and cached per host. The token is sent as `PRIVATE-TOKEN` only to the host it belongs to: `gitlab.Prepare` attaches it to API calls and to asset downloads whose URL host is known, and the client's `CheckRedirect` removes the header when a redirect leaves the original host (e.g. an object storage backend), because Go would otherwise copy custom headers across hosts. Unconfigured hosts never receive credentials. For gitlab.com the `GITLAB_TOKEN` variable is used; it is never sent to self-hosted instances.

**Limitations.** HTTPS only; no `http://` instances. The zipdown service receives the asset URL without credentials, so bare `.exe` assets of private GitLab projects cannot be wrapped. Tracked packages are keyed by `repo`, so the same `group/project` on two hosts collides.

---

## Asset selection

Asset selection is the most fragile part of the tool because release naming is not standardised. The heuristic runs in order:

1. If `--match` is provided, only assets matching that glob pattern are considered.
2. Otherwise, reject assets whose extension is not in the allowed set: `.zip`, `.tar.gz`, `.gz`, `.exe`.
3. Prefer assets whose name contains `windows` (case-insensitive).
4. Among matching assets prefer `amd64` or `x86_64` over `386` or `i386`.
5. If exactly one asset remains, use it. If more than one remains, pick the first match and log a warning listing the alternatives. If none remain, return an error with exit code 2.

The selection logic is isolated in `internal/asset` so it can be unit-tested independently without network calls.

**Why not interactive selection?** wpkget is designed to run unattended (e.g., in a scheduled task or a bootstrap script). Prompting the user breaks that use case. The `--dry-run` flag gives the user a way to inspect the choice before committing.

---

## Download and install pipeline

The pipeline is a linear sequence of steps. Each step receives the output of the previous one. Any error aborts the sequence and returns a descriptive message.

```
resolve asset URL
    → [optional] zipdown wrap (bare .exe only, when zipdown_url is set)
    → download to temp file (%TEMP%\wpkget\<random>)
    → decompress (if archive)
    → locate binary inside extracted contents  (or copy all with --all)
    → move binary to bin_dir
    → delete temp directory
    → update packages.yaml
```

**Temp directory:** `os.MkdirTemp` under `%TEMP%\wpkget\`. The temp dir is cleaned up with `defer os.RemoveAll(...)` so it is always removed even on error.

**Decompression:**
- `.zip` — `archive/zip` from the standard library.
- `.tar.gz` / `.gz` — `compress/gzip` + `archive/tar` from the standard library.
- `.exe` — no decompression; move directly (or route through zipdown when configured).

No third-party decompression library is used. The standard library covers all formats used by GitHub release assets.

**Binary identification inside archives:** After extraction, wpkget looks for `.exe` files in the extracted tree. If exactly one `.exe` is found it is used. If multiple are found, the one whose base name most closely matches the repository name is preferred (a simple `strings.Contains` check suffices). If none are found the user receives an actionable error.

**`--all` mode:** When the `all` flag is set (stored per-entry in `packages.yaml`), the entire archive contents are copied to `bin_dir` instead of extracting a single binary. If the archive has a single top-level directory it is unwrapped first, so the files land directly in `bin_dir`. This handles tools like neovim that ship a full directory tree.

**Timeouts:** the download client has no overall `Timeout`, because in Go it also covers reading the body and would abort large files over slow links or behind proxies that buffer the whole file (observed: 85 s before the first byte for a 104 MiB asset). Instead `downloadURL` uses a request context cancelled by a timer: `downloadHeaderTimeout` (10 min) until the headers arrive, then `downloadIdleTimeout` (2 min) re-armed on every successful read by `idleReader`. GitLab clients are copied with `Timeout = 0` for the same reason; their 120 s timeout still applies to API calls. The two values are package variables so tests can shorten them.

**Integrity check:** `verifyDownload` compares the file on disk with the `size` and `digest` (`sha256:<hex>`) fields of the GitHub release asset JSON, skipping any value that is absent. It runs only for direct downloads: zipdown returns a re-packaged zip whose bytes differ from the published asset, and GitLab release links carry neither field. It is not a signature check: it detects truncation and modification in transit (e.g. a proxy trickling or rewriting the body) but a compromised release would publish a matching size and digest. The `digest` field is a relatively recent GitHub addition; wpkget does not depend on it being present.

**Download feedback:** with the progress writer enabled, `downloadURL` prints `downloading <asset> from <host>` and runs `progress.StartWait` (a one-second ticker on stderr) until `client.Do` returns, then hands over to the bar.

**Progress bar:** `internal/progress` wraps the response body in an `io.Reader` that counts bytes and rewrites one stderr line with `\r` (at most every 100 ms, plus a final summary line). The total comes from `Content-Length`; when it is missing only counters are shown. It uses no dependency: ASCII bar, fixed line width (asset name shortened from the middle), no ANSI colors, because the terminal width cannot be queried without `golang.org/x/term` and ANSI is not portable across Windows consoles. Mode is `progress` in the config or `--progress`, with `auto` meaning "stderr is a character device". That check does not recognize mintty (Git Bash), where `always` is needed. Auto mode is also disabled by `--verbose`/`--debug`/`--debug-http`, since those logs share stderr. `install.Options.Progress` is the `io.Writer` to draw on (nil disables); zipdown downloads (`Wrap`) are not instrumented. If a download aborts, `Close` ends the line so later logs do not overwrite the bar.

**Modern output:** `internal/ui` is an opt-in presentation layer for `install` and `update`, enabled by `modern_term` or `--modern` and leaving the classic text untouched when off. Decisions worth knowing:

- *Feedback seam.* `install` does not know about either renderer: `Options.Feedback` (`install.Feedback`: `Waiting` before the request, `Wrap` around the body) is implemented by `ui.UI` for the modern output and by `legacyFeedback` for the classic one (which keeps the `downloading … from …` notice, the wait counter and the bar from `internal/progress`). `install.Run` also returns the downloaded size and elapsed time so callers can report them, and skips its own `dry-run:` lines when a `Feedback` is set because the caller prints a `(dry-run)` row.
- *Rows are streamed.* Each package prints its row when it finishes, not in a final pass, so a long `update` shows activity and an interrupted one keeps a coherent partial report. Column widths are computed up front from the package list (names exactly; versions with a 10-column minimum, so a longer new version only shifts its own row).
- *Terminal vs. color are separate.* Terminal detection (stdout is a character device and, on Windows, `ENABLE_VIRTUAL_TERMINAL_PROCESSING` could be enabled with `syscall`, no `x/sys`) decides whether to use the modern layout and cursor control; `NO_COLOR` only drops the color codes. A configured but non-terminal stdout falls back to the classic output; `--modern` forces it, which is the escape hatch for mintty, where Go cannot see a terminal.
- *Alignment.* Padding is applied to runes before coloring, because ANSI escapes would otherwise count as visible characters. Icons are Unicode, falling back to ASCII on a Windows console without `WT_SESSION` and without `TERM`, or with `WPKGET_ASCII` set; that heuristic is untested on real consoles.
- *Live line.* One line redrawn with `\r\x1b[2K` (waiting counter, then bar), erased before the row is printed. Downloads are sequential, so one line suffices; parallel downloads would need one line per transfer. It is disabled with `--verbose`/`--debug`/`--debug-http`, whose logs share the terminal, and with `--progress never`.
- *Glyph choice.* The check and cross are `✓` (U+2713) and `✗` (U+2717), not the heavier `✔` (U+2714) and `✖` (U+2716): the latter have an emoji presentation, and terminals that draw them wide while advancing one cell make them touch the following text. The others (`▲ • ⟳ → █ ░ ─`) have no emoji variant; `WPKGET_ASCII=1` remains the fallback if a font still misbehaves.
- *Display-only version cleanup.* `ui.CleanVersion` strips prefixes such as `ipinfo-` for the table; `packages.yaml` keeps the real tag.
- *Scope.* `check`, `list`, `show` and the rest keep the classic output; their tables use `text/tabwriter`, which would miscount ANSI escapes.

**Move vs. copy:** `os.Rename` is attempted first (atomic on the same volume). If it fails (cross-volume), the tool falls back to copy + delete. This avoids a partial binary at the destination during the move.

---

## zipdown integration

When the asset is a bare `.exe`, an optional zipdown service can intercept the download — useful for AV scanning, caching, or audit logging before the binary reaches the workstation.

**Design decision:** The zipdown code lives in `internal/zipdown` and is called from the install pipeline after asset selection, only when:
1. `zipdown_url` is set in config, and
2. the asset is a bare `.exe`.

The zipdown client makes a single authenticated `POST` request:

```
POST {zipdown_url}/wrap
Authorization: Bearer {zipdown_token}
Content-Type: application/json

{"url": "<asset_browser_download_url>"}
```

Response: a `.zip` file in the body. The client writes it to the temp dir and returns the path; the rest of the pipeline is unchanged.

When `zipdown_url` is empty the client returns `ErrNotConfigured` immediately, and the install pipeline falls back to downloading the `.exe` directly. Configuring zipdown requires no code changes — it is purely a config option.

---

## Package list format

YAML is chosen over JSON and TOML because:
- `gopkg.in/yaml.v3` is the only external dependency and is widely used in the Go ecosystem.
- YAML is human-editable with less visual noise than JSON for this use case.
- TOML would require another dependency with no additional benefit here.

Schema:

```yaml
packages:
  - repo: owner/name
    provider: gitlab           # optional: "github" (default) or "gitlab"
    host: git.example.com      # optional: self-hosted GitLab host (omitted for gitlab.com)
    version: "v1.2.3"
    binary_name: custom-name   # optional: installed .exe name (without extension)
    match: "*windows-amd64*"   # optional: glob used for asset selection
    all: true                  # optional: copy all archive contents instead of a single .exe
```

All optional fields are omitted from the file when empty/false. The file is read once at startup and written once at the end of a mutating command. There is no file locking — wpkget is not designed for concurrent invocations.

---

## Configuration

Config is loaded from (in priority order):
1. `--config` flag
2. `WPKGET_CONFIG` environment variable
3. `%APPDATA%\wpkget\config.yaml`

Missing config file is not an error; defaults are used. When the default path is used and the file does not exist, it is created with commented default values as a starting point. Fields:

| Field | Default | Description |
|-------|---------|-------------|
| `bin_dir` | `%APPDATA%\wpkget\bin` | Destination directory for installed binaries |
| `zipdown_url` | `""` | zipdown service URL; empty disables the feature |
| `zipdown_token` | `""` | Bearer token for the zipdown service |
| `user_agent` | `""` | Custom User-Agent for all HTTP requests |
| `modern_term` | `false` | Modern terminal output for `install` and `update` (see "Modern output") |
| `progress` | `auto` | Download progress bar: `auto`, `always` or `never` (see "Progress bar" under the install pipeline) |
| `gitlab_hosts` | `{}` | Self-hosted GitLab instances, keyed by host (see [GitLab](#gitlab)) |
| `pass_cmd` | `winpass` on Windows, `pass` elsewhere | pass-compatible binary used for `token_pass` lookups |

`--bin-dir` and `--user-agent` CLI flags override the corresponding config fields for the duration of the run.

### `config` subcommands

- **`open [packages]`** — ensures the file exists (config: written with defaults; packages: empty list), then opens it. The editor is `$VISUAL`, then `$EDITOR`, on all platforms; otherwise the OS handler (`rundll32 url.dll,FileProtocolHandler` on Windows, `open` on macOS, `xdg-open` elsewhere). The command line is split by a small quote-aware splitter (backslashes are literal so Windows paths work) and executed directly, never through a shell. The process is waited for so terminal editors work. Honoring `$EDITOR` on Windows is deliberate: users of Git Bash or other POSIX tooling usually already have it set.
- **`path [packages]`** — prints the effective path (`--config`, then `WPKGET_CONFIG`, then the platform default). The packages file is located with the same logic the other commands use, i.e. next to the `--config` path or in the default directory; note that this does not follow `WPKGET_CONFIG`.
- **`check`** — decodes the file with `yaml.Decoder.KnownFields(true)`, because the normal loader ignores unknown keys and a typo would otherwise fail silently. It then builds the session of each GitLab host (`gitlab.Check`) without any network access: CA bundle parsing and token lookup, including `pass_cmd`. Token values are only reported as found / not found. A missing file is not an error and is never created.
- **`show`** — loads the config through the regular loader and prints it as YAML with secrets masked. It reflects the file over defaults, not the per-run CLI overrides.

There is intentionally no `config set`: rewriting the file through `yaml.v3` drops the comments that document the generated default file.

---

## Error handling strategy

- Errors propagate up via `error` return values. No panics outside of true programmer errors.
- Each layer wraps errors with `fmt.Errorf("context: %w", err)` to preserve the chain.
- The CLI layer translates errors to human-readable messages and exits with a meaningful exit code.
- Network and API errors are not retried automatically; the error message includes enough context for the user to act.

---

## Dependencies

| Package | Reason |
|---------|--------|
| `gopkg.in/yaml.v3` | Parse and write YAML package list and config |

Everything else — HTTP client, archive handling, file I/O, flag parsing — uses the Go standard library.

**No GUI, no CGO, no shell.** External processes are started in only two cases: the configured `pass_cmd` when a `token_pass` entry is used, and the editor / OS file handler for `config open`. Both are executed directly with an argument vector, never through a shell. The binary must cross-compile cleanly with `GOOS=windows GOARCH=amd64`.

---

## Out of scope

- PATH management
- MSI, NSIS, or Squirrel installer support
- Rollback / uninstall of the binary itself
- Dependency resolution between packages
- Non-Windows platforms (the tool is Windows-only by design; the `_windows` build tag may be applied to platform-specific code if needed)
