# wpkget — Design Document

## Purpose

wpkget is a single-binary Windows CLI tool that automates the download and installation of compiled binaries distributed as GitHub release assets. It is intentionally narrow in scope: it does not manage PATH, does not handle version pinning beyond "track latest", and does not install MSI/NSIS installers.

---

## Repository layout

```
wpkget/
├── cmd/
│   └── wpkget/
│       └── main.go          # entry point, CLI wiring
├── internal/
│   ├── github/
│   │   └── github.go        # GitHub Releases API client
│   ├── asset/
│   │   └── asset.go         # asset selection heuristics
│   ├── install/
│   │   └── install.go       # download → decompress → move → cleanup
│   ├── packages/
│   │   └── packages.go      # local package list (read/write YAML)
│   ├── config/
│   │   └── config.go        # config file loading and defaults
│   └── zipdown/
│       └── zipdown.go       # zipdown client (stubbed until service is live)
└── go.mod
```

All packages under `internal/` are unexported to keep the public surface minimal and allow free refactoring.

---

## CLI design

The entry point uses the standard `flag` package with subcommand dispatch. No third-party CLI framework is used — the command set is small enough that the standard library suffices and avoids dependency bloat.

Subcommands: `install`, `update`, `list`, `remove`, `url`.

Global flags (`--config`, `--bin-dir`, `--dry-run`, `--verbose`) are parsed before the subcommand is dispatched.

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

## Asset selection

Asset selection is the most fragile part of the tool because release naming is not standardised. The heuristic runs in order:

1. Reject assets whose extension is not in the allowed set: `.zip`, `.tar.gz`, `.gz`, `.exe`.
2. Prefer assets whose name contains `windows` (case-insensitive).
3. Among matching assets prefer `amd64` or `x86_64` over `386` or `i386`.
4. If exactly one asset remains, use it. If more than one remains, pick the first match and log a warning listing the alternatives. If none remain, return an error with exit code 2.

The selection logic is isolated in `internal/asset` so it can be unit-tested independently without network calls.

**Why not interactive selection?** wpkget is designed to run unattended (e.g., in a scheduled task or a bootstrap script). Prompting the user breaks that use case. The `--dry-run` flag gives the user a way to inspect the choice before committing.

---

## Download and install pipeline

The pipeline is a linear sequence of steps. Each step receives the output of the previous one. Any error aborts the sequence and returns a descriptive message.

```
resolve asset URL
    → download to temp file (%TEMP%\wpkget\<random>)
    → decompress (if archive)
    → locate binary inside extracted contents
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

**Binary identification inside archives:** After extraction, wpkget looks for `.exe` files in the extracted tree. If exactly one `.exe` is found it is used. If multiple are found, the one whose base name most closely matches the repository name is preferred (Levenshtein distance is overkill; a simple `strings.Contains` check suffices). If none are found the user receives an actionable error.

**Move vs. copy:** `os.Rename` is attempted first (atomic on the same volume). If it fails (cross-volume), the tool falls back to copy + delete. This avoids a partial binary at the destination during the move.

---

## zipdown integration (future)

When the asset is an `.exe` and no decompression is needed, a direct move would work. However, the zipdown service provides an alternative path: upload the URL to the service, receive a `.zip` in return, then proceed through the normal decompression pipeline.

**Why bother?** zipdown is a custom service that may add value such as AV scanning, caching, or audit logging before the binary reaches the workstation. The tool should be ready to use it when it becomes available without a disruptive refactor.

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

Until the service is live, `zipdown.go` exposes the same interface but returns `ErrNotConfigured` immediately. The install pipeline treats `ErrNotConfigured` as a signal to proceed with the direct `.exe` download instead.

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
    version: "v1.2.3"
```

The file is read once at startup and written once at the end of a mutating command. There is no file locking — wpkget is not designed for concurrent invocations.

---

## Configuration

Config is loaded from (in priority order):
1. `--config` flag
2. `WPKGET_CONFIG` environment variable
3. `%APPDATA%\wpkget\config.yaml`

Missing config file is not an error; defaults are used. `bin_dir` defaults to `%APPDATA%\wpkget\bin` and is created if it does not exist.

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

**No GUI, no shell-out, no CGO.** The binary must cross-compile cleanly with `GOOS=windows GOARCH=amd64`.

---

## Out of scope

- PATH management
- MSI, NSIS, or Squirrel installer support
- Rollback / uninstall of the binary itself
- Dependency resolution between packages
- Non-Windows platforms (the tool is Windows-only by design; the `_windows` build tag may be applied to platform-specific code if needed)
