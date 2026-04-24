---
name: wallfacer-cli
description: Drive the Wallfacer platform from the shell via the `wallfacer` CLI. Covers auth, accounts, environments, snapshots, VMs (create/destroy/exec/logs), tasks with sessions/messages/attachments, and iOS simulator. TRIGGER when the user runs `wallfacer` commands, asks to authenticate or manage accounts, lists/creates/destroys VMs, runs commands on a VM, reads task sessions or messages, or parses `wallfacer` JSON output. SKIP unrelated CLIs (`aws`, `gcloud`, `wf` from the `droplet` project).
---

# wallfacer

Auto-generated CLI wrapping the Wallfacer API. YAML config under `~/.wallfacer/`, stdout is JSON by default (`-o json`). Every command accepts `--help`.

## Auth

```bash
wallfacer auth login --token=<your-token>
wallfacer auth status                        # validates token, lists accounts
wallfacer auth logout                        # removes stored token
```

Token source order: config file → `WALLFACER_TOKEN` env var.

Account-scoped calls require `account_id` in config or as a positional arg. If the user has one account, set it after login with `wallfacer auth status` to discover the ID.

## Configuration

Single config file at `~/.wallfacer/wallfacer.yml`:

```yaml
token: <sanctum token>
account_id: <uuid>
base_url: https://api.wallfacer.ai  # optional, defaults to https://api.wallfacer.ai
```

Environment variables with `WALLFACER_` prefix override config (e.g. `WALLFACER_TOKEN`, `WALLFACER_BASE_URL`).

When `account_id` is set, the account-id positional arg is auto-injected into all commands and can be omitted. See [references/config.md](references/config.md).

## Command groups

All commands are flat top-level groups (not nested). Write operations take JSON request bodies via **stdin** (piped), not flags. Use `-o json` for JSON output.

| Group | Read | Write |
|---|---|---|
| accounts | `list`, `get` | — |
| environments | `list`, `get <env-id>` | `create`, `update <env-id>`, `delete <env-id>` |
| snapshots | `list <env-id>`, `get <env-id> <snap-id>`, `logs <env-id> <snap-id>`, `log <env-id> <snap-id> <source>` | `create <env-id>`, `delete <env-id> <snap-id>` |
| vms | `list`, `get <vm-id>`, `logs <vm-id>`, `log <vm-id> <source>` | `create`, `delete <vm-id>`, `commands <vm-id>` |
| tasks | `list`, `get <task-id>` | `create`, `update <task-id>`, `delete <task-id>` |
| attachments | `list <task-id>`, `contents <task-id> <att-id>` | `create <task-id>`, `delete <task-id> <att-id>`, `refresh <task-id> <att-id>` |
| sessions | `list <task-id>`, `get <task-id> <sess-id>` | `create <task-id>`, `update <task-id> <sess-id>`, `abort <task-id> <sess-id>` |
| messages | `list <task-id> <sess-id>`, `get <task-id> <sess-id> <msg-id>` | `create <task-id> <sess-id>`, `delete <task-id> <sess-id> <msg-id>` |
| users | `list`, `get <user-id>` | `create`, `update <user-id>`, `delete <user-id>` |
| invitations | `list`, `get <token>` | `create`, `update <token>` |
| simulator | `simulator <vm-id>`, `simulator-screenshot <vm-id>`, `simulator-logs <vm-id>`, `simulator-builds <vm-id>` | — |

All positional args shown above assume `account_id` is set in config. If not, prepend the account UUID as the first positional arg to every command.

References: [config](references/config.md) · [environments](references/environments.md) · [vms](references/vms.md) · [tasks](references/tasks.md).

## Request bodies via stdin

The CLI does **not** support `--body`, `--name`, `--manifest-file`, or similar flags for passing data. Instead, pipe a JSON body via stdin:

```bash
# Inline JSON
echo '{"prompt": "fix the login bug", "environment_id": "<uuid>"}' | wallfacer tasks create

# From file (using jq to wrap manifest in create payload)
cat wf-dev-manifest.json | jq '{name: "my-env", manifest: .}' | wallfacer environments create

# From file directly
cat vm.json | wallfacer vms create
```

## Output format

Use `--output-format json` or `-o json` (not `--output json`):

```bash
wallfacer environments get <env-id> -o json
wallfacer vms list -o json
```

Default output is JSON. Also supports `-o yaml`.

## Destructive ops

`vms delete`, `environments delete`, `tasks delete`, `sessions abort` are irreversible. Confirm the target id with the user before running.

## Rate limits & pagination

List endpoints accept `--per-page` (max 100). The CLI does not follow pagination links automatically.

Batch jobs: watch for HTTP error responses indicating rate limiting.
