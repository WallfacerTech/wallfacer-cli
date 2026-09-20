# Team discovery, chat, and playbook runs

Three commands, two of which start work:

- `wallfacer team` finds who is in the account. Reads only, never creates a task.
- `wallfacer chat` starts a freeform conversation with one agent (`prompt`).
- `wallfacer run` starts a published playbook (`pipeline_id`).

Chat and run are separate on purpose. The task endpoint takes `prompt` or `pipeline_id`, never both, and neither command falls back to the other's mode.

All three use the `account_id` from config (or the `WALLFACER_ACCOUNT_ID` / `WF_ACCOUNT_ID` environment fallback). Without one they exit with `account_id not set in config`.

## Team references

Commands that take a `<reference>` accept any of:

| Form | Example |
|---|---|
| Member ID | `101` |
| Agent handle | `jin`, `@jin` |
| Email | `jin@example.test` |
| Unique display name | `"Grace Hopper"` |

Resolution tries the forms most specific first: ID, handle, email, then display name. A tier matching more than one record fails with every candidate's type, ID, name, and handle rather than picking one. Resolution reads both listings in full, so a teammate on the second page of results resolves the same as one on the first.

## `wallfacer team list`

```bash
wallfacer team list                       # agents and humans in one list
wallfacer team list --type agent          # agents only
wallfacer team list --include-disabled    # plus offboarded agents
wallfacer team list --max-pages 50        # widen the sweep
```

Sweeps the agent and user listings past the first page and returns one list. Each record carries:

- `type` — `agent` or `human`.
- `id`, `name`, `email`, `github_username`.
- `handle`, `title`, `role_page_id`, `environment_id`, `vendor`, `model`, `runtime_status` for agents; `role` for humans.
- `state` (`active`, `paused`, `disabled`) and `chatable`. Humans are always `active`: the members listing returns active memberships only, so somebody removed from the account is absent from the list rather than reported as removed.
- `follow_up` — the command for each reference in the record.

Offboarded agents are left out unless `--include-disabled` is passed. `pagination` reports how far each sweep got (`pages_read`, `complete`).

The directory does not aggregate anybody's tasks. It names the commands that do:

```bash
wallfacer handbook read <role-page-id>          # an agent's job description
wallfacer tasks list --created-by <agent-id>    # tasks running as that agent
wallfacer tasks list --owner-user-id <user-id>  # tasks a person owns
```

## `wallfacer team get <reference>`

Resolves one reference to a single record and reports how it matched (`resolved_from`), what type it is, and whether it can take work.

```bash
wallfacer team get jin
wallfacer team get "Grace Hopper" -q 'data.role_page_id' --raw
```

## `wallfacer chat <agent> [prompt]`

```bash
wallfacer chat jin "Look at the failing build on develop"
cat brief.md | wallfacer chat @auggie
wallfacer chat 101 "Review PR 67" --title "PR 67 review" --model claude-opus-5
```

Creates a task whose execution identity is the agent you named (`created_by`) and sends the prompt as its first message. The prompt comes from the second argument, or from stdin when it is omitted.

Flags: `--title`, `--environment-id`, `--harness`, `--model`, `--effort`, `--idle-timeout`.

Refusals, all by name and before anything is created:

- A human member. Humans are never a chat identity.
- A disabled or paused agent.
- A reference matching nothing, or matching more than one record.

Identity and environment beyond the agent you selected stay the server's: omit `--environment-id` and the task lands in the environment that agent works in.

## `wallfacer run <playbook>`

```bash
wallfacer run "Implement Assigned GitHub Issues"
wallfacer run <playbook-id> --message "Start with the checkout regression"
wallfacer run https://app.wallfacer.ai/accounts/<account-id>/handbook/<playbook-id>
```

Takes any playbook reference `wallfacer handbook` accepts and starts a task against it. The request carries `pipeline_id` and, when given, `message` — the kickoff instruction recorded as the run's triggering context. No version is pinned: the run uses the version the server has active.

Flags: `--message`, `--agent`, `--title`, `--environment-id`, `--idle-timeout`.

`--agent` sets the task's identity and default environment. It does not reassign step actors: each step runs as the actor the published version names, resolved by the server. The response says so under `step_actors`.

Refusals:

- A page passed where a playbook belongs (`"..." is a page, but a playbook was requested`).
- A playbook with a draft and nothing published. Publish it first; `wallfacer handbook draft <playbook-id>` reads the draft.
- An archived playbook.
- A URL from another account, refused before any request goes out.

A disabled playbook runs. Disabling clears its triggers so no event spawns a task, which makes a manual run the deliberate way to fire one; the response carries `disabled_note` saying so alongside the created task.

Running a playbook never edits it and never publishes its draft.

## What you get back

Both commands return the created task under `data`, with every identifier intact (`id`, `pipeline_id`, `pipeline_version_id`, `pipeline_version`), plus `mode` (`chat` or `playbook`), the agent or playbook that was selected, and `follow_up`:

```bash
wallfacer tasks get <task-id>
wallfacer sessions list <task-id>
wallfacer messages list <task-id> <session-id>
echo '{"content":"..."}' | wallfacer messages create <task-id> <session-id>
wallfacer handbook version <playbook-id> <version>   # runs only
```

There is no task view and no progress follower here: read the task and its sessions with the commands above.
