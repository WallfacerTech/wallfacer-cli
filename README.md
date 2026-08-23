# Wallfacer CLI

[![Go Report Card](https://goreportcard.com/badge/github.com/WallfacerTech/wallfacer-cli)](https://goreportcard.com/report/github.com/WallfacerTech/wallfacer-cli)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![GitHub Release](https://img.shields.io/github/v/release/WallfacerTech/wallfacer-cli)](https://github.com/WallfacerTech/wallfacer-cli/releases)
[![CI](https://github.com/WallfacerTech/wallfacer-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/WallfacerTech/wallfacer-cli/actions/workflows/ci.yml)

Command-line interface for [Wallfacer](https://wallfacer.ai) — manage cloud dev environments, tasks, and VMs from the terminal, CI, or scripts.

## Installation

### Quick install

```bash
curl -sSL https://raw.githubusercontent.com/WallfacerTech/wallfacer-cli/main/install.sh | sh
```

Detects your OS and architecture, downloads the latest release, and installs to `~/.local/bin`. Make sure that directory is on your `PATH`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

Or download manually from the [releases page](https://github.com/WallfacerTech/wallfacer-cli/releases/latest).

### From source

```bash
git clone https://github.com/WallfacerTech/wallfacer-cli.git
cd wallfacer-cli
make
```

## Authentication

Store your API token (from your [Wallfacer dashboard](https://app.wallfacer.ai)):

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

# Create a task (accepts JSON on stdin)
echo '{"prompt": "fix the login bug", "environment_id": "<env-id>"}' | wallfacer tasks create <account-id>

# Wait for snapshot and boot a VM in one step
wallfacer up <environment-id>

# List a session's messages, eliding image data and large tool blobs
wallfacer messages list <task-id> <session-id> --view trimmed

# Execute a command in a VM (shortcut)
wallfacer exec --vm <vm-id> -- ls -la /workspace
wallfacer exec --vm <vm-id> --dir /workspace --timeout 60 -- make build

# Get help for any command
wallfacer <command> --help
```

Run `wallfacer --help` to see all command groups, or `wallfacer <group> --help` for details on a specific group.

## Configuration

Configuration is stored in `~/.wallfacer/wallfacer.yml` (created automatically by `wallfacer auth login`):

```yaml
token: <your-api-token>
account_id: <default-account-id>  # optional — omit the account-id argument from commands
base_url: https://api.wallfacer.ai  # optional override
```

Config resolution is file-first with an environment-variable fallback: the config file
wins, and an environment variable only fills a field when it is absent from the file.
Each variable is accepted under either the `WALLFACER_` (documented, human-facing) or `WF_`
(harness-injected; `WF_` avoids Bunker's reserved `WALLFACER_` manifest-env prefix) spelling,
and `WALLFACER_` wins when both are set: `WALLFACER_TOKEN` / `WF_TOKEN` (→ `token`),
`WALLFACER_SERVER` / `WF_SERVER` (→ `base_url`), and `WALLFACER_ACCOUNT_ID` / `WF_ACCOUNT_ID`
(→ `account_id`). The env fallback lets ephemeral environments (e.g. Wallfacer harness VMs)
inject credentials with no config file present.

The CLI checks for updates automatically and prints a notice to stderr when a newer release is available.

## Development

Commands in `openapi.go` are auto-generated from `openapi.yaml` using [openapi-cli-generator](https://github.com/WallfacerTech/openapi-cli-generator). To regenerate after updating the spec:

```bash
make generate
```

This requires the openapi-cli-generator repo to be cloned alongside this one (as `../openapi-cli-generator`). `go.mod` uses a local `replace` directive, so `go install` from a remote module path won't work — build from a local clone.

## Agent Skills

The `skills/wallfacer-cli/` directory contains a [Claude Code](https://docs.anthropic.com/en/docs/claude-code) agent skill that teaches coding agents how to use this CLI. It includes:

- `SKILL.md` — Skill definition with command map and usage patterns
- `references/config.md` — Config file layout and auth
- `references/environments.md` — Environment and snapshot operations
- `references/tasks.md` — Tasks, sessions, messages, and attachments
- `references/vms.md` — VM lifecycle, exec, logs, and simulator
