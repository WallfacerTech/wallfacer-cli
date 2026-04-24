# Environments

An environment captures a project's build context: repo, platform, manifest, and snapshot state. Environments are the central concept — VMs boot from environments, and snapshots belong to environments.

## Read

```bash
wallfacer environments list
wallfacer environments get <environment-id>
```

## Create

```bash
echo '{
  "name": "my-app",
  "description": "Dev env for my-app",
  "git_repo": "https://github.com/org/my-app",
  "git_vendor": "github",
  "platform": "linux",
  "framework": "node",
  "manifest": { ... }
}' | wallfacer environments create
```

Or from a manifest file:

```bash
cat wf-dev-manifest.json | jq '{name: "my-app", manifest: .}' | wallfacer environments create
```

`git_vendor` ∈ {`github`, `gitlab`, `bitbucket`}. `manifest` is optional; skip for envs that do not need build overrides.

For the full AES manifest schema, see `../website/apps/docs/content/manifest-reference.md` on disk.

Returns the created environment, including its `id`.

## Update (PATCH semantics)

```bash
echo '{"framework": "python"}' | wallfacer environments update <environment-id>
```

Only the fields you include in the body are sent; omitted fields stay as-is.

## Delete

```bash
wallfacer environments delete <environment-id>
```

Irreversible. Fails if any VM still references the environment.

## Snapshot lifecycle

Creating an environment with a manifest triggers automatic snapshot generation. Poll the environment until `base_snapshot.status == "ready"` and `base_snapshot.id` is non-null:

```bash
wallfacer environments get <env-id> -o json
# Quick check:
wallfacer environments get <env-id> -o json | jq '{status: .data.base_snapshot.status, id: .data.base_snapshot.id}'
```

Expected progression: `"generating"` → `"ready"`. Simple manifests ~80s, Docker manifests ~15 min.

## Snapshots

Snapshots are a separate top-level command group, scoped to an environment:

```bash
wallfacer snapshots list <environment-id>
wallfacer snapshots create <environment-id>
wallfacer snapshots get <environment-id> <snapshot-id>
wallfacer snapshots delete <environment-id> <snapshot-id>
wallfacer snapshots logs <environment-id> <snapshot-id>        # list log sources
wallfacer snapshots log <environment-id> <snapshot-id> <source> # read a specific log
```

Snapshot logs work for both successful and failed snapshots — use them to diagnose generation failures.
