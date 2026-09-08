---
name: wallfacer-cli
description: Drive the Wallfacer platform from the shell via the `wallfacer` CLI. Covers auth, accounts, environments, snapshots, VMs (create/destroy/exec/logs), tasks with sessions/messages/attachments, handbook pages (`wallfacer pages` — the only write path for handbook pages, since the `handbook_*` MCP tools are read-only), and iOS simulator. TRIGGER when the user runs `wallfacer` commands, asks to authenticate or manage accounts, lists/creates/destroys VMs, runs commands on a VM, reads task sessions or messages, asks to create/update/delete a handbook page or playbook page, or parses `wallfacer` JSON output. SKIP unrelated CLIs (`aws`, `gcloud`, `wf` from the `droplet` project).
---

# wallfacer

Auto-generated CLI wrapping the Wallfacer API. YAML config under `~/.wallfacer/`, stdout is JSON by default (`-o json`). Every command accepts `--help`.

## Auth

```bash
wallfacer auth login --token=<your-token>
wallfacer auth status                        # validates token, lists accounts
wallfacer auth logout                        # removes stored token
```

Token source order: config file → `WALLFACER_TOKEN` (or `WF_TOKEN`) env var (file wins; env only fills when absent).

Account-scoped calls require `account_id` in config or as a positional arg. If the user has one account, set it after login with `wallfacer auth status` to discover the ID.

## Configuration

Single config file at `~/.wallfacer/wallfacer.yml`:

```yaml
token: <sanctum token>
account_id: <uuid>
base_url: https://api.wallfacer.ai  # optional, defaults to https://api.wallfacer.ai
```

Config resolution is file-first with an environment-variable fallback (file wins; env only fills a field when it is absent from the file): `WALLFACER_TOKEN` / `WF_TOKEN` → `token`, `WALLFACER_SERVER` / `WF_SERVER` → `base_url`, `WALLFACER_ACCOUNT_ID` / `WF_ACCOUNT_ID` → `account_id`. Each var is accepted under either the `WALLFACER_` (documented) or `WF_` (harness-injected) spelling; `WALLFACER_` wins when both are set.

When `account_id` is set, the account-id positional arg is auto-injected into all commands and can be omitted. See [references/config.md](references/config.md).

## Shortcuts

### up

Polls an environment until its base snapshot is ready, creates a VM, and waits for it to boot. Handles snapshot propagation retries automatically.

```bash
wallfacer up <environment-id>
wallfacer up <environment-id> --poll 10 --max-wait 300
```

Flags: `--poll` (interval in seconds, default 5), `--max-wait` (timeout in seconds, default 600). Prints VM ID and port mappings when ready.

### exec

Shortcut for running commands in a VM without piping JSON to stdin. Everything after `--` is the shell command:

```bash
wallfacer exec --vm <vm-id> -- ls -la /workspace
wallfacer exec --vm <vm-id> --dir /workspace --timeout 60 -- make build
```

Flags: `--vm` (required), `--dir` (working directory), `--timeout` (seconds, 1-300). Requires `account_id` in config.

## Command groups

All commands are flat top-level groups (not nested). Write operations take JSON request bodies via **stdin** (piped), not flags. Use `-o json` for JSON output.

| Group | Read | Write |
|---|---|---|
| accounts | `list`, `get`, `handbook` | — |
| pages | `list`, `get <page-id>` | `create`, `update <page-id>`, `delete <page-id>` |
| revisions | `list <page-id>`, `get <page-id> <revision-id>` | — |
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

## Handbook pages

Handbook pages are Wallfacer **pages**. The `handbook_*` MCP tools an agent gets in a session read pages only, so `wallfacer pages` is the write path for creating, editing, moving, and deleting them.

```bash
wallfacer accounts handbook                  # the whole handbook as a nested tree
wallfacer pages list                         # flat, paginated
wallfacer pages get <page-id>                # includes the markdown body
echo '{"title": "PR bodies", "body": "..."}' | wallfacer pages create
echo '{"body": "..."}' | wallfacer pages update <page-id>
wallfacer pages delete <page-id>
```

`update` takes any of `title`, `body`, `parent_page_id`, `position`, and `deleted: false` (restores a deleted page). `delete` keeps the page's revision history and re-parents its children to the deleted page's parent. Revision history is `wallfacer revisions list <page-id>` / `get <page-id> <revision-id>`.

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
