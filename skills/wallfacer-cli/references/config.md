# Config

## File layout

`0600` YAML at `~/.wallfacer/wallfacer.yml`:

```yaml
base_url: https://api.wallfacer.ai
token: <sanctum token>
account_id: <uuid>
```

## Resolution precedence (file-first, env fallback)

The config file wins. An environment variable only fills a field when that field is
absent from the file — so a human's `~/.wallfacer/wallfacer.yml` always takes precedence,
while ephemeral environments (e.g. Wallfacer harness VMs) can inject credentials with no
file present.

Each env var is accepted under either the `WALLFACER_` (documented, human-facing) or `WF_`
(harness-injected; `WF_` avoids Bunker's reserved `WALLFACER_` manifest-env prefix) spelling.
`WALLFACER_` wins when both are set.

| Field | Config (wins) | Env fallback | Default |
|---|---|---|---|
| `token` | `token` | `WALLFACER_TOKEN` / `WF_TOKEN` | — |
| `base_url` | `base_url` | `WALLFACER_SERVER` / `WF_SERVER` | `https://api.wallfacer.ai` |
| `account_id` | `account_id` | `WALLFACER_ACCOUNT_ID` / `WF_ACCOUNT_ID` | — (must be set or passed as positional arg) |

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
