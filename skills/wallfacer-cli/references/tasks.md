# Tasks, sessions, messages, attachments

Four top-level command groups: `tasks`, `sessions`, `messages`, `attachments`. Each is flat (not nested under `tasks`).

## Tasks

```bash
wallfacer tasks list -o json
wallfacer tasks get <task-id> -o json

echo '{
  "prompt": "Refactor auth",
  "environment_id": "<uuid>",
  "title": "Fix login redirect"
}' | wallfacer tasks create

echo '{"title": "New title"}' | wallfacer tasks update <task-id>
wallfacer tasks delete <task-id>
```

If `prompt` is provided on create, a session starts immediately and the prompt becomes the first message. Omit `prompt` to create the task without starting a session.

Optional fields: `environment_id` (default env for sessions), `idle_timeout_seconds` (60-900, default 300), `attachments` (array of MCP-aligned attachment objects), `created_by` (user ID).

## Sessions

Sessions belong to tasks. They can be created explicitly or are auto-created when a task has a prompt.

```bash
wallfacer sessions list <task-id> -o json
wallfacer sessions get <task-id> <session-id> -o json
echo '{"environment_id": "<uuid>"}' | wallfacer sessions create <task-id>
echo '{"status": "paused"}' | wallfacer sessions update <task-id> <session-id>
wallfacer sessions abort <task-id> <session-id>
```

`abort` is destructive — the session's VM is destroyed and cannot be reopened. Confirm the id with the user first.

## Messages

```bash
wallfacer messages list <task-id> <session-id> -o json
wallfacer messages get <task-id> <session-id> <message-id> -o json
echo '{"content": "check the tests"}' | wallfacer messages create <task-id> <session-id>
wallfacer messages delete <task-id> <session-id> <message-id>
```

Message list is cursor-paginated (`--per-page`, max 100). Returns all transcript rows including non-conversational marker types (`attachment`, `last-prompt`, `queue-operation`, `ai-title`). Conversation UIs should filter to `user` and `assistant` roles.

## Attachments

Attachments are MCP-aligned resources attached to tasks. GitHub issue/PR URIs are resolved automatically.

```bash
wallfacer attachments list <task-id> -o json
echo '{
  "name": "PROJ-42 Login bug",
  "uri": "https://github.com/acme-corp/frontend/issues/42"
}' | wallfacer attachments create <task-id>
wallfacer attachments contents <task-id> <attachment-id>
wallfacer attachments refresh <task-id> <attachment-id>
wallfacer attachments delete <task-id> <attachment-id>
```

`contents` returns MCP `ResourceContents` shape. `refresh` re-resolves the URI (e.g. fetches latest issue state from GitHub).
