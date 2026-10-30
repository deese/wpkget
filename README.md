# wpkget

A minimal package manager that pulls compiled binaries directly from GitHub and GitLab releases (gitlab.com and self-hosted instances). It targets Windows first and also builds for Linux and macOS.

## Installation

```
go install github.com/deese/wpkget/src/cmd/wpkget@latest
```

Or download the latest release archive for your platform (`wpkget_<os>_<arch>.zip` on Windows, `.tar.gz` elsewhere) and place the binary in a directory on your `PATH`. `wpkget version` prints the installed version.

## Configuration

wpkget looks for its config file at a platform-specific default location.
Override with the `--config` flag or the `WPKGET_CONFIG` environment variable.
The file is created automatically with commented defaults on first run.

| OS      | Default path |
|---------|-------------|
| Windows | `%APPDATA%\wpkget\config.yaml` |
| Linux   | `$XDG_CONFIG_HOME/wpkget/config.yaml` (fallback: `~/.config/wpkget/config.yaml`) |
| macOS   | `$XDG_CONFIG_HOME/wpkget/config.yaml` (fallback: `~/.config/wpkget/config.yaml`) |

The default `bin_dir` is also platform-specific:

| OS      | Default `bin_dir` |
|---------|------------------|
| Windows | `%APPDATA%\wpkget\bin` |
| Linux / macOS | `~/.local/bin` |

```yaml
# config.yaml
bin_dir: "C:\\tools\\bin"       # destination folder for installed binaries

# Optional: zipdown service for AV scanning / caching of bare .exe assets.
zipdown_url: ""
zipdown_token: ""

# User-Agent sent with every HTTP request (GitHub API + asset downloads).
# Defaults to "wpkget". Override per-run with --user-agent.
user_agent: "wpkget"

# Download progress bar: auto (terminals only), always or never.
# Override per run with --progress.
progress: auto

# Modern terminal output (colors, aligned columns, live progress, summary).
# Off by default; override per run with --modern / --no-modern.
modern_term: false

# Optional: self-hosted GitLab instances (see "GitLab" below).
gitlab_hosts:
  git.example.com:
    token_pass: gitlab/example
pass_cmd: winpass
```

## Usage

### Install a package

Download and install the latest release of a GitHub or GitLab repository.

```
wpkget install <user/repo> [...]
         [--provider <p>]     source provider: github (default) or gitlab
         [--name <binary>]    rename the installed binary (single repo only)
         [--match <pattern>]  glob to select the release asset
         [--all]              copy all archive contents to bin-dir
         [--bin-dir <path>]   destination directory for this package (saved per-package)
```

Example:

```
wpkget install junegunn/fzf
wpkget install cli/cli
wpkget install BurntSushi/ripgrep --match "*windows-msvc*.zip"
wpkget install neovim/neovim --name nvim
wpkget install https://gitlab.com/group/project
```

wpkget selects the release asset built for your operating system and architecture, among files ending in `.zip`, `.tar.gz`, `.gz` or `.exe` (and bare extensionless binaries on Linux and macOS). Use `--match` when the choice is ambiguous; `wpkget show <repo>` lists the available assets, and `--verbose` prints how the choice was made.
The binary is extracted (if compressed) and moved to `bin_dir`. The archive is deleted afterward.

### Download a release without installing it

`get` is a one-off download: it resolves, downloads, verifies and extracts a release like `install` does, but leaves the result in the current directory. It never touches `bin_dir` or `packages.yaml`, so nothing becomes tracked or updatable.

```
wpkget get <user/repo> [...]
         [--tag/-t <tag>]     release tag to download (default: the latest release)
         [--out/-o <dir>]     output directory (default: the current directory)
         [--match <pattern>]  glob to select the release asset
         [--name <binary>]    name for the extracted binary (single repo only)
         [--all]              extract the whole archive into <repo>-<tag>/
         [--no-extract]       keep the downloaded file as is
         [--force]            overwrite an existing destination
         [--provider/-p <p>]  source provider: github (default) or gitlab
```

Examples:

```
wpkget get deese/wpkget                 # latest wpkget binary, next to your terminal
wpkget get cli/cli --tag 2.67.0         # a specific release ("v2.67.0" is tried too)
wpkget get BurntSushi/ripgrep --all -o D:\tmp
wpkget get junegunn/fzf --no-extract    # keep the .zip untouched
```

The path of what was written is printed on stdout and nothing else, so it can be used in scripts (`$(wpkget get cli/cli)`); the download notice and the progress bar go to stderr. With the modern output a `↓` row is shown instead, followed by the path.

- Without `--tag` the latest release is used. With `--tag` the exact tag is requested first and, if it does not exist, the same tag with the `v` prefix added or removed (`1.2.3` and `v1.2.3`).
- An existing destination is refused before anything is downloaded, unless `--force` is given. With `--all` a directory that exists and is empty is accepted.
- The same checks as `install` apply: size and SHA-256 verification, timeouts, and your proxy's own inspection.
- It does not replace running programs. To update wpkget itself, `get` the new binary and swap it manually: `wpkget get deese/wpkget` leaves `wpkget.exe` in the current directory. If that is the running binary, Windows will normally refuse to overwrite it even with `--force`, so run it from another directory or use `--out`.

### GitLab

The repo argument selects the provider:

| Argument | Provider |
|----------|----------|
| `owner/repo` | GitHub (default) |
| `owner/repo --provider gitlab` | gitlab.com |
| `https://github.com/owner/repo` | GitHub |
| `https://gitlab.com/group/project` | gitlab.com |
| `https://<host>/group/subgroup/project` | self-hosted GitLab (`<host>` must be listed in `gitlab_hosts`) |

Self-hosted instances are declared in `config.yaml`. URLs for any other host are rejected, so a token can never be sent to an unexpected server.

```yaml
gitlab_hosts:
  git.example.com:
    prefix: /gitlab                # only if GitLab is served under a subpath
    token_env: GITLAB_TOKEN_EXAMPLE  # environment variable holding the token
    token_pass: gitlab/example     # or a pass/winpass entry (first line is used)
    ca_file: ${EXAMPLE_CA_FILE}    # extra PEM CA bundle; $VAR / ${VAR} are expanded
    skip_verify: false             # disable TLS verification (insecure, prefer ca_file)
pass_cmd: winpass                  # defaults to winpass on Windows, pass elsewhere
```

| Key | Description |
|-----|-------------|
| `prefix` | URL path under which GitLab is served (e.g. `/gitlab`). Empty for root installs. |
| `token_env` | Name of an environment variable with the access token. Checked first. |
| `token_pass` | `pass`-compatible entry; `<pass_cmd> show <entry>` is run and the first line is used. Used when `token_env` is empty or unset. |
| `ca_file` | PEM bundle trusted in addition to the system roots. When empty, the `WPKGET_CA_FILE` environment variable is used. |
| `skip_verify` | Skips TLS certificate verification for this host and logs a warning. |

Notes:

- The token is only sent to the host it is configured for, and is dropped on redirects to a different host.
- For `gitlab.com` the `GITLAB_TOKEN` environment variable is used (unless `gitlab.com` is also listed in `gitlab_hosts`). It is never sent to self-hosted instances.
- Instances are reached over HTTPS only.
- The tracked package is identified by `repo`, so the same `group/project` on two different hosts cannot be tracked at once.

### Get the download URL for a repository

Print the resolved download URL without downloading anything.

```
wpkget url <repo> [...]
         [--provider <p>]     source provider: github (default) or gitlab
```

### Show package details

Print the provider, latest release, tracking status (version, per-package options, pinned) and every release asset with its download URL. Useful to pick a `--match` pattern.

```
wpkget show <repo> [...]
         [--provider <p>]     source provider: github (default) or gitlab
```

Example output:

```
repo:     junegunn/fzf
provider: github
latest:   v0.57.0
tracked: v0.56.0 (update available)

assets (12):
  fzf-0.57.0-windows_amd64.zip  https://github.com/junegunn/fzf/releases/download/...
```

### Update packages

Check all tracked packages for new releases and install the newer ones. The package list is saved after each package, so an interrupted run keeps what was already updated.

```
wpkget update [<repo> ...]
```

If a newer version is found the full install process runs automatically (download, decompress, move, delete).

Pass one or more repos to update only those. They must already be tracked (a plain `owner/repo` or a full URL; a URL must also match the tracked host). Untracked repos are reported and the exit code is 1; pinned packages (`keep_version`) are still skipped.

### Check for available updates (without installing)

Show the latest available version for each tracked package without downloading anything.

```
wpkget check
```

### List tracked packages

```
wpkget list [-v]
```

Output shows each tracked package and its currently installed version.
Pass `-v` to also show the resolved download URL.

### Manage the config file

```
wpkget config open [packages]   open the file for editing
wpkget config path [packages]   print the file path
wpkget config check             validate the config
wpkget config show              print the loaded config
```

`open` launches `$VISUAL`, then `$EDITOR` (arguments and double quotes are supported, e.g. `EDITOR="code -w"`), on every platform. When neither is set it falls back to the OS default handler (the file association on Windows, `open` on macOS, `xdg-open` on Linux). The file is created with defaults if it does not exist yet. GUI editors that return immediately (such as `code` without `-w`) are fine: wpkget just waits for the process to exit.

The optional `packages` argument of `open` and `path` targets `packages.yaml` instead of `config.yaml`.

`check` parses the config in strict mode, so a misspelled key (e.g. `gitlab_host`) is reported instead of being silently ignored, and then verifies every `gitlab_hosts` entry offline: the `ca_file` must be a readable PEM bundle and a configured `token_pass` entry must be retrievable (this runs `pass_cmd`, which may prompt for a passphrase). Token values are never printed. It exits with code 1 if anything is wrong, and does not create the config file if it is missing.

`show` prints the config as loaded (file values over defaults) with `zipdown_token` masked. It does not apply `--bin-dir` or `--user-agent` overrides.

### Remove a package from tracking

```
wpkget remove <user/repo>
```

> This removes the entry from the local package list but does **not** delete the binary.

## Package list

wpkget maintains a package list in `packages.yaml`, stored next to the effective config file (by default `%APPDATA%\wpkget\packages.yaml` on Windows). `wpkget config path packages` prints the location.

```yaml
packages:
  - repo: junegunn/fzf
    version: "0.57.0"
  - repo: cli/cli
    version: "2.68.1"
  - repo: BurntSushi/ripgrep
    version: "14.1.1"
    match: "*windows-msvc*.zip"
  - repo: neovim/neovim
    version: "v0.10.4"
    binary_name: nvim
    all: true
  - repo: someuser/sometool
    version: "1.0.0"
    bin_dir: "D:\\custom\\path"
  - repo: group/subgroup/project
    provider: gitlab
    host: git.example.com        # omitted for github.com and gitlab.com
    version: "v1.2.3"
```

The file is updated automatically after each successful install or update.

## Flags

| Flag | Description |
|------|-------------|
| `--config <path>` | Path to config file |
| `--bin-dir <path>` | Override destination directory (global or per-package when used with `install`) |
| `--user-agent <ua>` | Override User-Agent header for HTTP requests |
| `--dry-run` | Resolve and print what would be done without doing it |
| `--verbose` | Enable verbose output |
| `--debug` | Print step-by-step diagnostic output |
| `--debug-http` | Dump outgoing HTTP requests and response bodies |
| `--modern` / `--no-modern` | Force the modern terminal output on or off for this run. Overrides `modern_term` |
| `--quiet`, `-q` | Modern output only: hide up-to-date and pinned packages (the summary still counts them) |
| `--progress <mode>` | Download progress bar: `auto` (default), `always` or `never`. Overrides the `progress` config key |

### Download behavior

Before each download wpkget prints `downloading <asset> from <host>` and, until the first byte arrives, a live `waiting for server... Ns` counter (same visibility rules as the progress bar below). This matters behind proxies that buffer the whole file before forwarding it, where the wait can last over a minute.

There is no overall download timeout, so large files over slow links work. Two limits apply instead: 10 minutes to receive the response headers, and 2 minutes without receiving any data once the body has started.

When the provider publishes the asset size (GitHub does) wpkget checks that the downloaded file has exactly that size, and it also checks the SHA-256 when the API reports a `digest`. A mismatch aborts the install with a message such as `size mismatch … (the download was truncated or modified in transit)`. Nothing is checked for assets whose size or digest is not reported (GitLab release links, and downloads routed through zipdown).

Some proxies treat unknown clients differently, for example by trickling a partial file while it is scanned. wpkget identifies itself with the `user_agent` setting (default `wpkget`), which a proxy rule can match.

### Modern output

With `modern_term: true` in the config (or `--modern` for one run), `install` and `update` print a colored, column-aligned report instead of one plain line per package:

```
 ⟳ wpkget · d:\CustomTools\bin

 ✓ deese/tfdown                          v1.3.0
 ✓ burntsushi/ripgrep                    15.2.0
 ▲ google-antigravity/antigravity-cli    1.4.8     → 1.4.10      60.2 MiB    0.7s
 ▲ ipinfo/cli                            3.3.2     → 3.3.4        5.2 MiB    0.1s
 • neovim/neovim                         v0.10.4   pinned
 ✗ foo/bar
     timeout

 ──────────────────────────────────────────────
 ▲ 2 updated  ✓ 2 up to date  • 1 pinned  ✗ 1 errors   65.4 MiB · 0.8s
```

Rows are printed as each package finishes, so a long `update` shows progress and an interrupted one still shows what was done. While a download runs, a single live line shows `waiting for server... Ns` until the first byte arrives and then a bar (`████████░░░░  64%  149.5/233.6 MiB`); it is erased when the row is printed. Version tags are shown without their prefix (`ipinfo-3.3.2` becomes `3.3.2`); `packages.yaml` keeps the full tag.

Colors: green for up to date and new versions, yellow for updates, red for old versions and errors, gray for secondary text. Rules:

- It is used only when stdout is a terminal. If it is not (a file, a pipe), the classic output is kept so logs stay stable. `--modern` skips that detection, which is needed in terminals Go cannot recognize, such as Git Bash (mintty).
- `NO_COLOR` removes the colors but keeps the layout.
- Unicode icons are used by default. On a Windows console that is neither Windows Terminal nor an xterm-like emulator, or with `WPKGET_ASCII=1`, ASCII icons (`+ ^ x`) are used.
- With `--verbose`, `--debug`, `--debug-http` or `--progress never` the live line is not drawn (the rows and summary still are).
- `check`, `list`, `show` and the other commands keep the classic output.

### Progress bar

This section describes the classic bar, used when the modern output is off.

Downloads draw a one-line progress bar on stderr:

```
winpass_windows_amd64.zip  [##########----------]  50%  2.0/4.0 MiB  1.0 MiB/s
```

When the server does not report the size, only the counters are shown. When the download ends the line is replaced by a summary with the total size and elapsed time.

In `auto` mode the bar is drawn only when stderr is a terminal, so logs and pipes stay clean, and it is hidden while `--verbose`, `--debug` or `--debug-http` are active because their output shares stderr. Terminals that Go cannot recognize as such, notably Git Bash (mintty), need `--progress always` (or `progress: always` in the config). Set `progress: never` in the config to turn it off permanently. Downloads routed through the zipdown service are not shown.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Asset not found for the repository |
| 3 | Network or API error |
