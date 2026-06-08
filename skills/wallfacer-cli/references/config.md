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

## Profiles

A profile is a named `{base_url, token, account_id}` triple for switching between
backends (e.g. prod vs. a local Sophon running on `localhost:8080`). Profiles are
optional and fully backward compatible — with no profile selected, the top-level
keys behave exactly as documented above (the implicit default).

```yaml
# Top-level keys remain the implicit default (used when no profile is selected).
token: <prod token>
account_id: <prod account uuid>

profiles:
  develop:
    base_url: http://localhost:8080
    token: <local sophon token>
    account_id: <local account uuid>
  staging:
    base_url: https://staging.api.wallfacer.ai
    token: <staging token>
```

Select a profile per-call with the `--profile` flag, or per-session with the
`WALLFACER_PROFILE` / `WF_PROFILE` env var (there is no persisted "current profile"):

```bash
wallfacer accounts list --profile develop
WALLFACER_PROFILE=develop wallfacer environments list
```

Resolution precedence for each field: **the active profile's field (wins) → the
`WALLFACER_`/`WF_` env var → empty** (`base_url` then defaults to prod). A profile
never falls back to the top-level keys — those belong to the default only, so a
prod `account_id` can't bleed into a local profile. The global `--server` flag
still overrides the URL for a single call, beating the profile's `base_url`.

Profile names are case-insensitive. Selecting a profile that isn't defined is a
hard error. Inspect with:

```bash
wallfacer profile list       # all profiles; * marks the active one
wallfacer profile current    # active profile + resolved server/account/token state
```

Profiles are defined by editing `~/.wallfacer/wallfacer.yml` directly (`auth login`
still writes the top-level default keys).

## Auth commands

```bash
wallfacer auth login --token=<token>   # saves token to config
wallfacer auth status                   # validates token, lists accessible accounts
wallfacer auth logout                   # removes token from config
```

Prefer `wallfacer auth login` over editing the config file directly.

## Account ID auto-injection

When `account_id` is set in config, all commands that take `account-id` as their first positional arg have it auto-injected. The arg is removed from the command's usage string, so `wallfacer vms list account-id` becomes just `wallfacer vms list`.
