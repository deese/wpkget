# wpkget

A minimal Windows package manager that pulls compiled binaries directly from GitHub releases.

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

```yaml
# config.yaml
bin_dir: "C:\\tools\\bin"       # destination folder for installed binaries

# Optional: zipdown service for AV scanning / caching of bare .exe assets.
zipdown_url: ""
zipdown_token: ""

# User-Agent sent with every HTTP request (GitHub API + asset downloads).
# Defaults to "wpkget". Override per-run with --user-agent.
user_agent: "wpkget"
```

## Usage

### Install a package

Download and install the latest Windows release from a GitHub repository.

```
wpkget install <user/repo> [...]
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
```

wpkget selects the release asset whose name contains `windows` and whose extension is `.zip`, `.tar.gz`, `.gz`, or `.exe`.
The binary is extracted (if compressed) and moved to `bin_dir`. The archive is deleted afterward.

### Get the download URL for a repository

Print the resolved download URL without downloading anything.

```
wpkget url <user/repo>
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
| 3 | Network or GitHub API error |
