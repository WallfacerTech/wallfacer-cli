# WF Dev

Consolidated notes for creating and using Wallfacer dev environments.
Updated 2026-04-30.

## Quick Start

```bash
# Create environment from a manifest.
cat wf-dev-manifest.json \
  | jq '{name: "my-env", manifest: .}' \
  | ./wallfacer environments create

# Add secrets to the environment (required once per environment).
ENV_ID=<env-id>
ACCT_ID=$(cat ~/.wallfacer/wallfacer.yml | grep account_id | awk '{print $2}')
TOKEN=$(cat ~/.wallfacer/wallfacer.yml | grep token | awk '{print $2}')
jq -c '.secrets[]' wf-dev-secrets.json | while read secret; do
  curl -sS "http://localhost:8080/v1/accounts/$ACCT_ID/environments/$ENV_ID/secrets" \
    -X POST -H "Authorization: Bearer $TOKEN" \
    -H "Accept: application/json" -H "Content-Type: application/json" \
    -d "$secret" | jq '{name: .data.name, id: .data.id}'
done

# Wait for snapshot, create VM, and wait for boot (all-in-one).
./wallfacer up <env-id>

# Or do it manually:
# ./wallfacer environments get <env-id> -o json \
#   | jq '{status: .data.base_snapshot.status, id: .data.base_snapshot.id}'
# echo '{"environment_id":"<env-id>","snapshot_id":"<snapshot-id>"}' \
#   | ./wallfacer vms create
# ./wallfacer vms get <vm-id> -o json \
#   | jq '.data | {status, ready}'

# External URLs (e.g. Sophon API) are automatically injected into .env files
# by the manifest service `run` commands via ${WALLFACER_PORT_URL_SOPHON_API}
# substitution — no manual post-boot injection needed.

# Run commands (exec shortcut).
./wallfacer exec --vm <vm-id> -- ls /workspace

# Or via stdin (still works).
echo '{"command":"ls /workspace","timeout":10}' \
  | ./wallfacer vms commands <vm-id>

# Cleanup. Currently returns HTTP 500, but idle reaping still works.
./wallfacer vms delete <vm-id>
```

## Rules That Matter

- VM create needs both `environment_id` and `snapshot_id`. Without `snapshot_id`, bunker creates a bare VM with empty `/workspace` and no GitHub credentials.
- Do not pass the raw manifest to VM create. Manifest setup runs during snapshot generation only.
- Snapshot readiness is two phase: wait for `.data.base_snapshot.status == "ready"` and a non-null snapshot ID, then wait another 5-15s for Redcoast propagation. If VM create says "Snapshot not found", retry after 10-15s.
- Booted VMs report `status: "running"` and `ready: true`.
- `--platform` is the app category, not the OS. The VM OS comes from `platform.os` in the manifest.
- Manifest `env` values do not propagate to `vms commands` shells. Set environment in setup steps or inline commands.
- Request bodies are read from stdin. The CLI does not support `--body`, `--name`, or `--manifest-file` for these calls.
- Use `--output-format json` or `-o json`, not `--output json`.

## Manifests

| File | Purpose | Snapshot time |
|---|---|---|
| `wf-dev-manifest-1-docker.json` | Clone repos, start Docker with overlay2, activate pnpm | ~45s |
| `wf-dev-manifest-2-deps.json` | Docker plus Sophon `.env` and Nova `auth.json` | ~45s |
| `wf-dev-manifest-3-build.json` | Build Sail image, install deps, generate key, migrate and seed | ~4.5m |
| `wf-dev-manifest.json` | Full build plus services, commands, and ready steps | ~4.5m |

`wf-dev-manifest.json` is the recommended end-to-end manifest. Docker daemon
and containers persist through Firecracker snapshot and restore.

## Latest Linux Findings

Validated 2026-04-30 against `wf-dev-manifest.json` on Linux/Firecracker.

- Current good environment: `dev-full-0429-noselenium-3`
  (`019dda15-f06c-7321-a146-4c14492bec9d`), ready snapshot
  `459dda02-d93d-46fb-9606-7c470414a584`.
- Current live VM: `019dda1c-b09e-7268-982a-f39b5aa6c64b`.
  `sophon-api`: `http://100.84.222.126:10047`; `app-web`:
  `http://100.84.222.126:10048`.
- Selenium must be avoided in both setup and runtime. Use
  `docker compose run --rm --no-deps -T laravel.test ...` during setup, and
  start runtime services with `docker compose up -d --no-deps laravel.test ...`
  after explicitly starting only `pgsql` and `redis`. Pulling/starting the
  Selenium service adds avoidable image/runtime failures.
- `app-web` auto-starts with the correct external Sophon URL injected into
  `.env.development` by the manifest service `run` command via
  `${WALLFACER_PORT_URL_SOPHON_API}` substitution. No manual post-boot
  injection needed.
- Platform friction observed: VM API can still report `status: "active"` even
  when ready; `platform.os` selects Linux/macOS while CLI `--platform` means app
  category; manifest `env` values are not reliable in later command shells, so
  persist values into files or inline command env where needed.

## Docker

The VM image does not auto-start Docker. Start it in setup:

```json
{
  "name": "Start Docker and activate pnpm",
  "run": "sudo dockerd --storage-driver=overlay2 &>/tmp/dockerd.log & for i in $(seq 1 60); do docker info >/dev/null 2>&1 && break; sleep 1; done && corepack enable && corepack prepare pnpm@10.19.0 --activate",
  "healthcheck": {
    "command": "docker info --format '{{.ServerVersion}}' && pnpm --version"
  }
}
```

Use `--storage-driver=overlay2`. For build-only container commands, use
`docker compose run --no-deps`; for runtime, use `docker compose up --no-deps`
for app services after starting only their required infrastructure. This keeps
Selenium and other unrelated Compose services out of snapshot setup and VM boot.

## VM Image

Preinstalled: Debian `node:slim`, Node.js 22, Docker + Compose v2, Git,
GitHub CLI, Python 3, Claude Code CLI, tmux, jq, and curl.

Runtime details:

- User: `appuser` / UID `1001`
- Workspace: `/workspace`
- Not installed: pnpm, composer, PHP
- Activate pnpm with corepack in setup
- Run composer and PHP inside the Sail container

## Sophon Manifest Requirements

- Sophon's `.env` is gitignored. Fresh VM setup still needs to create one.
- `.env.ci` is the right base, but override `DB_HOST=pgsql`,
  `REDIS_HOST=redis`, `WWWUSER=1001`, and `WWWGROUP=1001` for Docker Compose.
- Laravel Nova is still a private Composer dependency. Write `auth.json`
  before `composer install`.

## Known Issues

| Issue | Status | Workaround |
|---|---|---|
| Docker not auto-started | Open | Start `dockerd` in manifest setup |
| `vms delete` returns HTTP 500 | Open | Let idle reaping clean up |
| Long `vms commands` hit gateway timeout at ~60s | Open | Run long work in manifest setup or background it and poll |
| Stale snapshot VM blocks rebuild | Known | Delete `snapshot-gen-env-<envId>` through bunker |

VMs are reaped by Sophon's `DestroyIdleVmsJob`, not Redcoast. The default
timeout is 5 minutes, the configured range is 1-15 minutes, and local dev has
used 30 minutes in Sophon's `.env`.

## Debugging

```bash
# Environment and snapshot status.
./wallfacer environments get <env-id> -o json
./wallfacer snapshots list <env-id>
./wallfacer snapshots logs <env-id> <snapshot-id>

# Update an existing environment manifest; this triggers snapshot rebuild.
echo '{"manifest": <manifest-json>}' \
  | ./wallfacer environments update <env-id>

# Inspect recent snapshot logs from local Sophon.
cd ../sophon
docker compose exec -T laravel.test php artisan tinker \
  --execute="foreach (\DB::table('vm_logs')->latest('created_at')->take(15)->get() as \$l) { echo \$l->virtual_machine_id.' | '.\$l->event.' | '.\$l->level.' | '.substr(\$l->payload ?? '', 0, 300).' | '.\$l->created_at.\"\n\"; }"
```

## Prerequisites

- CLI built and authenticated: `make build && ./wallfacer auth status`
- Local Sophon running with migrations applied
- Target branches pushed to GitHub; VMs clone from remote, not local
