# wpkget

A minimal Windows package manager that pulls compiled binaries directly from GitHub and GitLab releases (gitlab.com and self-hosted instances).

## Installation

```
go install github.com/deese/wpkget@latest
```

Or download the latest release binary and place it in a directory on your `PATH`.

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

# Optional: self-hosted GitLab instances (see "GitLab" below).
gitlab_hosts:
  git.example.com:
    token_pass: gitlab/example
pass_cmd: winpass
```

## Usage

### Install a package

Download and install the latest Windows release from a GitHub repository.

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

wpkget selects the release asset whose name contains `windows` and whose extension is `.zip`, `.tar.gz`, `.gz`, or `.exe`.
The binary is extracted (if compressed) and moved to `bin_dir`. The archive is deleted afterward.

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

### Check for updates

Check all tracked packages for new releases.

```
wpkget update
```

If a newer version is found the full install process runs automatically (download, decompress, move, delete).

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

### Remove a package from tracking

```
wpkget remove <user/repo>
```

> This removes the entry from the local package list but does **not** delete the binary.

## Package list

wpkget maintains a package list at `%APPDATA%\wpkget\packages.yaml`.

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

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Asset not found for the repository |
| 3 | Network or API error |
