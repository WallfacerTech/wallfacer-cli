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

# Browse the handbook: pages and playbooks in one surface
wallfacer handbook tree
wallfacer handbook search "pull request"
wallfacer handbook read "R&D/Engineering/Build"
wallfacer handbook read <playbook-id>          # record plus the full active definition
wallfacer handbook resolve "Writing Great PRs" # name, path or URL -> stable ID and type

# Edit a page and organize the tree
wallfacer handbook create "Writing Great PRs" --body-file pr.md --under "R&D/Engineering"
wallfacer handbook update "R&D/Engineering/Build" --body-file build.md
wallfacer handbook move <playbook-id> --under "R&D/Engineering"

# Author a playbook: draft, compare, publish
wallfacer handbook save-draft <playbook-id> --definition-file draft.yaml
wallfacer handbook diff-draft <playbook-id>    # draft vs the active published definition
wallfacer handbook publish <playbook-id> --notes "Added the smoke-test step"

# Find a teammate: agents and human members in one directory
wallfacer team list
wallfacer team get jin                         # id, handle, email or name -> one record

# Start work: a conversation with an agent, or a published playbook
wallfacer chat jin "Look at the failing build on develop"
wallfacer run "Implement Assigned GitHub Issues" --message "Start with #66"

# Execute a command in a VM (shortcut)
wallfacer exec --vm <vm-id> -- ls -la /workspace
wallfacer exec --vm <vm-id> --dir /workspace --timeout 60 -- make build

# Get help for any command
wallfacer <command> --help
```

Run `wallfacer --help` to see all command groups, or `wallfacer <group> --help` for details on a specific group.

### Handbook

`wallfacer handbook` lists, searches, reads, edits, and organizes the account's handbook pages and playbooks. References accept a stable ID, a unique name, a full path (`R&D/Engineering/Build`), a Wallfacer page or playbook detail URL inside the configured account, or a `wallfacer://handbook/pages/<id>` link. Names and paths resolve against active entries; an ambiguous name is reported with its candidates rather than guessed at.

```bash
wallfacer handbook list --type playbook            # flat, with each entry's path and state
wallfacer handbook list --page 2                   # traverse past the first page
wallfacer handbook search "review" --limit 5
wallfacer handbook read <page-id> -q 'data.body' --raw
wallfacer handbook versions <playbook-id>          # published versions, newest first
wallfacer handbook version <playbook-id> 3         # one published version in full
wallfacer handbook draft <playbook-id>             # the unpublished draft, on its own
wallfacer handbook revisions <page-id>
```

Reads never substitute an unpublished draft for a playbook's active definition: `handbook read` reports only that a draft exists, and `handbook draft` returns its content. Every result carries a `follow_up` object naming the next command. Where the result names one record — a read, a resolve, a write, a `revisions` or `versions` listing — every entry runs as printed, with the ids already filled in. Where it names many (`tree`, `list`, `search`), the entries give the command's shape and you fill the reference in from the record you picked; `next_page` is the same, since which page to ask for is the caller's choice. Either way the next read is available from one result plus `--help`.

Page edits and hierarchy changes use the same references:

```bash
wallfacer handbook create "Writing Great PRs" --body-file pr.md --under "R&D/Engineering"
echo '{"title":"Release","body":"..."}' | wallfacer handbook create   # stdin JSON body
wallfacer handbook update <page-id> --title "Writing great PRs"
wallfacer handbook move <playbook-id> --under "R&D/Engineering" --position 0
wallfacer handbook move <page-id> --top-level
wallfacer handbook reorder --under "R&D/Engineering" "Build" <playbook-id> "Review"
wallfacer handbook delete <page-id>
wallfacer handbook restore <page-ref>           # refused unless the page is deleted
```

A page write is live knowledge immediately: agents running playbooks read the new content from the next task onward. History is per editing session, not per edit: consecutive saves by the same author within ten minutes of that session's first save collapse into one revision holding the session's latest body, and a different author or a later save starts a new one. So the result's `revisions` command reads back the wording as of an earlier session, not whatever the page said before your last save, and intermediate wordings from inside one session are not recoverable.

Deleting a page keeps its revision history and does not delete what is filed under it: sub-pages and playbooks move up to the deleted page's parent, or to the top level, and the result names each one. `restore` brings the page back with its content and history intact; it takes the same reference forms as every other command and refuses a page that is not deleted, which in practice means an ID, since names and paths resolve against active entries only.

`reorder` writes one parent's child order in a single atomic request. Pages and playbooks share one ordering under a parent, so the list is mixed and must be that parent's complete set of children; a list that omits, repeats, or imports a sibling is rejected before anything is written. `move` a playbook and only its parent and position change: no draft save, no publish, no trigger change, and no task.

### Authoring playbooks

Only `create-playbook` and `publish` change a playbook's versioned definition. Everything else either reads, or changes metadata that takes effect without a version.

```bash
wallfacer handbook create-playbook --name "Fix a bug" --definition-file playbook.yaml
wallfacer handbook save-draft <playbook-id> --definition-file draft.yaml   # not published
wallfacer handbook diff-draft <playbook-id>                                # draft vs active
wallfacer handbook diff <playbook-id> 2 3                                  # two published versions
wallfacer handbook publish <playbook-id> --notes "Added the smoke-test step"
wallfacer handbook publish <playbook-id> --activate=false                  # version it, don't activate
wallfacer handbook discard-draft <playbook-id>                             # active version untouched
wallfacer handbook update-playbook <playbook-id> --disable
wallfacer handbook archive-playbook <playbook-id>
wallfacer handbook restore-playbook <playbook-id>                          # comes back disabled; refused unless archived
```

Creation publishes: the definition given to `create-playbook` is validated, stored as version 1, and made active in the same call. Publishing operates on the saved draft and fails without creating a version when there is no draft, when the stored draft holds no definition, or when the server's validation rejects it. Publishing never enables a disabled playbook, and `--activate=false` reports the new version separately from the still-active one.

Page revisions and playbook versions are different histories. A page's current content, and the playbook's set of linked pages, reach every later run as soon as they are saved. A playbook's steps and triggers reach later runs only when a version is published, and a task that is already running stays pinned to the version it was created against.

Definitions are read from `--definition-file` (JSON or YAML) or from stdin, and the bare definition, the API's `{"definition": ...}` envelope, and what `handbook version` or `handbook draft` print are all accepted, so a definition read out of the CLI goes straight back into a draft. To submit a definition directly without saving a draft, the low-level `wallfacer versions create <playbook-id>` command still takes one.

### Team

`wallfacer team` is the account's directory: its agents and its human members in one list, each record carrying its type, identity, role data, and the IDs the other commands take. Both listings are swept past the first page. References accept a member ID, an agent handle (with or without a leading `@`), an email address, or a unique display name, and an ambiguous reference is reported with its candidates rather than guessed at.

```bash
wallfacer team list --type agent            # the actor picker
wallfacer team list --include-disabled      # plus offboarded agents
wallfacer team get "Grace Hopper"
wallfacer team get jin -q 'data.role_page_id' --raw
```

The directory reports no task counts of its own. It names the commands that do: `wallfacer handbook read <role-page-id>` for an agent's job description, and `wallfacer tasks list --created-by <agent-id>` or `--owner-user-id <user-id>` for the work.

### Chat and run

Two ways to start a task, kept separate because they are two different asks. `chat` sends the freeform `prompt` contract to an agent you name; `run` sends `pipeline_id` for a published playbook. Neither ever builds the other's request.

```bash
wallfacer chat jin "Look at the failing build on develop"
cat brief.md | wallfacer chat @auggie        # prompt from stdin
wallfacer run <playbook-id> --message "Start with the checkout regression"
wallfacer run https://app.wallfacer.ai/accounts/<account-id>/handbook/<playbook-id>
```

`chat` is agent-directed: a human member, and an agent that is disabled or paused, are refused by name rather than quietly becoming the identity on the task. `run` takes any playbook reference `handbook` accepts, uses the version the server has active, and refuses a page, an archived playbook, and one with a draft and nothing published. Its `--agent` sets the task's identity and default environment; the playbook's steps still run as the actors its published version names.

Both return the created task with its identifiers intact and a `follow_up` object naming the `tasks get`, `sessions list`, and `messages list` commands for what they started, plus a `messages create` reply. A run adds `handbook read` for the playbook and `handbook version` for the version it is executing; a chat has neither.

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

Product commands — `up`, `exec`, `chat`, `run`, and the `handbook` and `team` groups — are hand-maintained outside `openapi.go` so that regenerating the spec never drops them. Run the tests with:

```bash
go test ./...
```

## Agent Skills

The `skills/wallfacer-cli/` directory contains a [Claude Code](https://docs.anthropic.com/en/docs/claude-code) agent skill that teaches coding agents how to use this CLI. It includes:

- `SKILL.md` — Skill definition with command map and usage patterns
- `references/config.md` — Config file layout and auth
- `references/environments.md` — Environment and snapshot operations
- `references/tasks.md` — Tasks, sessions, messages, and attachments
- `references/vms.md` — VM lifecycle, exec, logs, and simulator
- `references/handbook.md` — Finding and reading pages and playbooks
- `references/team.md` — Team discovery, agent chat, and playbook runs
