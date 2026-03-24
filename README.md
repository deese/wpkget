# wpkget

A minimal Windows package manager that pulls compiled binaries directly from GitHub releases.

## Installation

```
go install github.com/deese/wpkget@latest
```

Or download the latest release binary and place it in a directory on your `PATH`.

## Configuration

wpkget looks for its config file at `%APPDATA%\wpkget\config.yaml` by default.
Override with the `--config` flag or the `WPKGET_CONFIG` environment variable.

```yaml
# config.yaml
bin_dir: "C:\\tools\\bin"       # destination folder for installed binaries
zipdown_url: ""                 # zipdown service base URL (future)
zipdown_token: ""               # zipdown auth token (future)
```

## Usage

### Install a package

Download and install the latest Windows release from a GitHub repository.

```
wpkget install <user/repo>
```

Example:

```
wpkget install junegunn/fzf
wpkget install cli/cli
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

### List tracked packages

```
wpkget list
```

Output shows each tracked package and its currently installed version.

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
```

The file is updated automatically after each successful install or update.

## Flags

| Flag | Description |
|------|-------------|
| `--config <path>` | Path to config file |
| `--bin-dir <path>` | Override destination directory for this run |
| `--dry-run` | Resolve and print what would be done without doing it |
| `--verbose` | Enable verbose output |

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Asset not found for the repository |
| 3 | Network or GitHub API error |
