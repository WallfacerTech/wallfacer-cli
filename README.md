# wallfacer

[![Go Report Card](https://goreportcard.com/badge/github.com/WallfacerTech/wallfacer-cli)](https://goreportcard.com/report/github.com/WallfacerTech/wallfacer-cli)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![GitHub Release](https://img.shields.io/github/v/release/WallfacerTech/wallfacer-cli)](https://github.com/WallfacerTech/wallfacer-cli/releases)
[![CI](https://github.com/WallfacerTech/wallfacer-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/WallfacerTech/wallfacer-cli/actions/workflows/ci.yml)

Command-line interface for [Wallfacer](https://wallfacer.ai) — cloud dev environments for coding agents. Wallfacer runs Claude inside a real cloud dev environment with your repo, services, Docker, Xcode, and the iOS Simulator. Control the whole task from any device, CI workflow, or API.

Built with [openapi-cli-generator](https://github.com/WallfacerTech/openapi-cli-generator), all commands are auto-generated from the [Wallfacer API](https://api.wallfacer.ai) OpenAPI spec.

## Installation

### Quick install

```bash
curl -sSL https://raw.githubusercontent.com/WallfacerTech/wallfacer-cli/main/install.sh | sh
```

Detects your OS and architecture, downloads the latest release, and installs to `/usr/local/bin`. Or download manually from the [releases page](https://github.com/WallfacerTech/wallfacer-cli/releases/latest).

### From source

```bash
git clone https://github.com/WallfacerTech/wallfacer-cli.git
cd wallfacer-cli
make
```

## Authentication

Store your API token (created via `POST /v1/tokens`):

```bash
wallfacer auth login --token=<your-token>
```

Check that it works:

```bash
wallfacer auth status
```

## Usage

```bash
# List accounts
wallfacer accounts list

# Get a specific account
wallfacer accounts get <account-id>

# List environments for an account
wallfacer environments list <account-id>

# Create a task
echo '{"prompt": "fix the login bug"}' | wallfacer tasks create <account-id>

# Wait for snapshot and boot a VM in one step
wallfacer up <environment-id>

# Execute a command in a VM (shortcut)
wallfacer exec --vm <vm-id> -- ls -la /workspace
wallfacer exec --vm <vm-id> --dir /workspace --timeout 60 -- make build

# Get help for any command
wallfacer <command> --help
```

Every command group has its own `--help`. Run `wallfacer --help` to see all available groups, or `wallfacer <group> --help` for details on a specific group (e.g. `wallfacer auth --help`).

If you set `account_id` in the config file, the account-id argument is automatically injected and can be omitted.

## Configuration

Configuration is stored in `~/.wallfacer/wallfacer.yml`:

```yaml
token: <your-api-token>
account_id: <default-account-id>
base_url: https://api.wallfacer.ai  # optional override
```

Environment variables with the `WALLFACER_` prefix are also supported (e.g. `WALLFACER_TOKEN`).

## Agent Skills

The `skills/wallfacer-cli/` directory contains a Claude Code agent skill that teaches agents how to use this CLI. It includes:

- `SKILL.md` — Skill definition with command map and usage patterns
- `references/config.md` — Config file layout and auth
- `references/environments.md` — Environment and snapshot operations
- `references/tasks.md` — Tasks, sessions, messages, and attachments
- `references/vms.md` — VM lifecycle, exec, logs, and simulator

## Development

The CLI commands in `openapi.go` are generated from `openapi.yaml`. To regenerate after updating the spec:

```bash
make generate
```

This requires the [openapi-cli-generator](https://github.com/WallfacerTech/openapi-cli-generator) repo to be cloned alongside this one (as `../openapi-cli-generator`).

> **Note:** `go.mod` uses a `replace` directive pointing to `../openapi-cli-generator` because the generator repo is currently private. `go install` from a remote module path will not work until the generator repo is made public. For now, build from a local clone with the sibling repo present.
