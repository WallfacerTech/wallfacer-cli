---
name: wallfacer-cli
description: Drive the Wallfacer platform from the shell via the `wallfacer` CLI. Covers auth, accounts, environments, snapshots, VMs (create/destroy/exec/logs), tasks with sessions/messages/attachments, the handbook (`wallfacer handbook` to find, read, edit, and organize pages and playbooks, and to draft and publish playbooks, since the `handbook_*` MCP tools are read-only; `wallfacer pages` is the low-level route to the same endpoints), the team directory (`wallfacer team`), starting work (`wallfacer chat` with an agent, `wallfacer run` for a published playbook), and iOS simulator. TRIGGER when the user runs `wallfacer` commands, asks to authenticate or manage accounts, lists/creates/destroys VMs, runs commands on a VM, reads task sessions or messages, asks to find or read a handbook page or playbook, asks to create/update/delete a handbook page or playbook page, asks to draft or publish a playbook definition, asks who is on the team or which agents exist, asks to message an agent or hand it work, asks to run a playbook, or parses `wallfacer` JSON output. SKIP unrelated CLIs (`aws`, `gcloud`, `wf` from the `droplet` project).
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

All commands are flat top-level groups (not nested). Write operations take JSON request bodies via **stdin** (piped), not flags; the `handbook` write commands also accept stdin and layer their own flags over it. Use `-o json` for JSON output.

| Group | Read | Write |
|---|---|---|
| team | `list`, `get <reference>` | — |
| handbook | `tree`, `list`, `search <query>`, `read <ref>`, `resolve <ref>`, `revisions <page-ref>`, `revision <page-ref> <rev-id>`, `versions <playbook-ref>`, `version <playbook-ref> [version]`, `draft <playbook-ref>`, `diff <playbook-ref> <a> <b>`, `diff-draft <playbook-ref>` | `create [title]`, `update <page-ref>`, `delete <page-ref>`, `restore <page-id>`, `move <ref>`, `reorder <ref>...`, `create-playbook`, `update-playbook <playbook-ref>`, `archive-playbook <playbook-ref>`, `restore-playbook <playbook-id>`, `save-draft <playbook-ref>`, `discard-draft <playbook-ref>`, `publish <playbook-ref>` |
| accounts | `list`, `get`, `handbook` | — |
| pages | `list`, `get <page-id>` | `create`, `update <page-id>`, `delete <page-id>` |
| revisions | `list <page-id>`, `get <page-id> <revision-id>` | — |
| environments | `list`, `get <env-id>` | `create`, `update <env-id>`, `delete <env-id>` |
| snapshots | `list <env-id>`, `get <env-id> <snap-id>`, `logs <env-id> <snap-id>`, `log <env-id> <snap-id> <source>` | `create <env-id>`, `delete <env-id> <snap-id>` |
| vms | `list`, `get <vm-id>`, `logs <vm-id>`, `log <vm-id> <source>` | `create`, `delete <vm-id>`, `commands <vm-id>` |
| tasks | `list`, `get <task-id>` | `create`, `update <task-id>`, `delete <task-id>`, and the two entry points `chat <agent> [prompt]` and `run <playbook>` |
| attachments | `list <task-id>`, `contents <task-id> <att-id>` | `create <task-id>`, `delete <task-id> <att-id>`, `refresh <task-id> <att-id>` |
| sessions | `list <task-id>`, `get <task-id> <sess-id>` | `create <task-id>`, `update <task-id> <sess-id>`, `abort <task-id> <sess-id>` |
| messages | `list <task-id> <sess-id>`, `get <task-id> <sess-id> <msg-id>` | `create <task-id> <sess-id>`, `delete <task-id> <sess-id> <msg-id>` |
| users | `list`, `get <user-id>` | `create`, `update <user-id>`, `delete <user-id>` |
| invitations | `list`, `get <token>` | `create`, `update <token>` |
| simulator | `simulator <vm-id>`, `simulator-screenshot <vm-id>`, `simulator-logs <vm-id>`, `simulator-builds <vm-id>` | — |

All positional args shown above assume `account_id` is set in config. If not, prepend the account UUID as the first positional arg to every command.

References: [config](references/config.md) · [environments](references/environments.md) · [vms](references/vms.md) · [tasks](references/tasks.md) · [handbook](references/handbook.md) · [team](references/team.md).

## Handbook

`wallfacer handbook` is the entry point for the account's handbook: pages and playbooks in one surface, with every result carrying the references needed for the next command, and the place its playbooks are authored. No command in the group creates a task.

```bash
wallfacer handbook tree                       # pages and playbooks as one nested tree
wallfacer handbook list --type playbook       # flat, with each entry's path and state
wallfacer handbook search "pull request"      # both types, title + description + page body
wallfacer handbook read "Engineering/Build"   # page body, or a playbook's active definition
wallfacer handbook resolve <ref>              # a name, path, or URL -> stable ID, type, state
```

Page authoring and tree organization take the same references:

```bash
wallfacer handbook create "PR bodies" --body-file pr.md --under "Engineering"
echo '{"title":"PR bodies","body":"..."}' | wallfacer handbook create
wallfacer handbook update "Engineering/Build" --body-file build.md
wallfacer handbook move <playbook-id> --under "Engineering" --position 0
wallfacer handbook reorder --under "Engineering" "Build" <playbook-id> "Review"
wallfacer handbook delete <page-ref>          # children move up; history is kept
wallfacer handbook restore <page-id>          # deleted pages are reached by ID
```

A page write is live knowledge immediately, and `handbook revisions` holds one row per editing session, not per edit: same-author saves within ten minutes of the session's first save update that row in place with the latest body, so the wording you just overwrote in the same sitting is not recoverable. `reorder` is one atomic write of a parent's complete, mixed child list. Moving a playbook changes only its parent and position: no draft save, no publish, no trigger change, no task.

Playbook authoring splits saving from publishing. `save-draft` stores a working copy and changes nothing about how the playbook runs; `publish` sends that saved draft to the version endpoint and is the only command besides `create-playbook` that changes the versioned definition.

```bash
wallfacer handbook save-draft <playbook-ref> --definition-file draft.yaml
wallfacer handbook diff-draft <playbook-ref>  # local comparison against the active version
wallfacer handbook publish <playbook-ref> --notes "Added the smoke-test step"
wallfacer handbook discard-draft <playbook-ref>
```

Publishing fails without creating a version when there is no saved draft or the server rejects it, never enables a disabled playbook, and with `--activate=false` reports the new version separately from the active one. A page's content and a playbook's linked pages reach later runs as soon as they are saved, with no publish; a task already running stays pinned to the playbook version it was created against.

References accept a stable ID, a unique name, a full path (`R&D/Engineering/Build`), a page or playbook detail URL inside the configured account, or a `wallfacer://handbook/pages/<id>` link. Ambiguous names are reported with their candidates rather than guessed at, and a URL from another account is refused before any request goes out. Pass `--type page` or `--type playbook` when a page and a playbook share a name.

Read the full detail in [references/handbook.md](references/handbook.md).

## Team, chat, and playbook runs

`wallfacer team` is the directory: the account's agents and its human members in one list, carrying the IDs the other commands take. `wallfacer chat` and `wallfacer run` are the two ways to start work, and they are separate commands because they are two different asks.

```bash
wallfacer team list                       # agents and humans, swept past page one
wallfacer team list --type agent          # the actor picker
wallfacer team get jin                    # id, handle, email or name -> one record
wallfacer chat jin "Look at the failing build"                              # sends `prompt`
wallfacer run "Implement Assigned GitHub Issues" --message "Start with #66" # sends `pipeline_id`
```

- **Chat is agent-directed.** A human member, and a disabled or paused agent, are refused by name rather than quietly becoming the identity on the task.
- **Run uses the version the server has active.** No version is pinned, a draft-only playbook is refused, and `--agent` is the task's identity and default environment rather than an override of the playbook's step actors.
- **Neither falls back to the other.** Chat never sends `pipeline_id`; run never sends `prompt`.
- **Both return the created task plus `follow_up`** naming the `tasks get`, `sessions list`, and `messages list` commands for what they started, and a `messages create` reply. A run adds `handbook read` for the playbook and `handbook version` for the version it is executing; a chat has neither.

Reading the directory never creates a task, and running a playbook never edits or publishes it. Read the full detail in [references/team.md](references/team.md).

The low-level `wallfacer pages` group still works unchanged and is the raw route to the same endpoints (the `handbook_*` MCP tools an agent gets in a session read pages only). Playbook writes are in the `handbook` group above:

```bash
echo '{"title": "PR bodies", "body": "..."}' | wallfacer pages create
echo '{"body": "..."}' | wallfacer pages update <page-id>
wallfacer pages delete <page-id>
```

`update` takes any of `title`, `body`, `parent_page_id`, `position`, and `deleted: false` (restores a deleted page). `delete` keeps the page's revision history and re-parents its children to the deleted page's parent.

## Request bodies via stdin

Outside the `handbook` group the CLI does **not** support `--body`, `--name`, `--manifest-file`, or similar flags for passing data. Instead, pipe a JSON body via stdin:

```bash
# Inline JSON
echo '{"prompt": "fix the login bug", "environment_id": "<uuid>"}' | wallfacer tasks create

# From file (using jq to wrap manifest in create payload)
cat wf-dev-manifest.json | jq '{name: "my-env", manifest: .}' | wallfacer environments create

# From file directly
cat vm.json | wallfacer vms create
```

`handbook create` and `handbook update` are the exception: they read the same JSON body from stdin and also accept `--title`, `--body`, `--body-file`, `--position`, `--under`, and `--top-level`. A flag wins over the same field in a piped body, and a body flag (`--body`, `--body-file`, `--clear-body`) skips the stdin read entirely.

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

List endpoints serve 25 per page by default. The CLI does not follow pagination links automatically; you either page it yourself or use a command that sweeps.

`--page` and `--per-page` are registered on `wallfacer handbook list`, `wallfacer handbook revisions`, and `wallfacer handbook versions`, and `pagination` passes through each underlying endpoint's own `meta` and `links`, so `--page` traverses with the API's numbers. `--per-page` caps at 200 on `handbook list` (both the pages and the playbooks listing) and on `handbook versions`, and at 100 on `handbook revisions`. Generated list commands that expose `--per-page` state that endpoint's cap in `--help`.

`wallfacer handbook search` and `wallfacer team list` take neither flag. They sweep their listings themselves at a fixed 100 records per request, bounded by `--max-pages` (default 20), and report `pages_read` and `complete` per source under `pagination`. Resolving a team reference (`team get`, `chat`, and `run --agent`) sweeps both listings in full, with no page ceiling.

Batch jobs: watch for HTTP error responses indicating rate limiting.
