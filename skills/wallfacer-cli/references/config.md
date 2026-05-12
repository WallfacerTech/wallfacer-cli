# Config

## File layout

`0600` YAML at `~/.wallfacer/wallfacer.yml`:

```yaml
base_url: https://api.wallfacer.ai
token: <sanctum token>
account_id: <uuid>
```

## Resolution precedence (highest → lowest)

| Field | Env var | Config | Default |
|---|---|---|---|
| `token` | `WALLFACER_TOKEN` | `token` | — |
| `base_url` | `WALLFACER_BASE_URL` | `base_url` | `https://api.wallfacer.ai` |
| `account_id` | — | `account_id` | — (must be set or passed as positional arg) |

Missing token → auth commands fail. Missing account on an account-scoped call → the account-id positional arg is required.

## Auth commands

```bash
wallfacer auth login --token=<token>   # saves token to config
wallfacer auth status                   # validates token, lists accessible accounts
wallfacer auth logout                   # removes token from config
```

Prefer `wallfacer auth login` over editing the config file directly.

## Account ID auto-injection

When `account_id` is set in config, all commands that take `account-id` as their first positional arg have it auto-injected. The arg is removed from the command's usage string, so `wallfacer vms list account-id` becomes just `wallfacer vms list`.
