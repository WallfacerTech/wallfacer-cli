# Handbook discovery and reading

`wallfacer handbook` finds and reads the account's handbook: pages and playbooks in one surface. Every command in the group issues reads only. It never creates a task and never changes handbook content or state.

All commands use the `account_id` from config (or the `WALLFACER_ACCOUNT_ID` / `WF_ACCOUNT_ID` environment fallback). Without one they exit with `account_id not set in config`.

## References

Commands that take a `<reference>` accept any of:

| Form | Example |
|---|---|
| Stable ID | `019ec8c5-5424-70e0-b5b1-39998a11f0f5` |
| Unique name | `"Writing Great PRs"` |
| Full path | `"R&D/Engineering/Build"` |
| Page detail URL | `https://app.wallfacer.ai/accounts/<account-id>/handbook/pages/<page-id>` |
| Playbook detail URL | `https://app.wallfacer.ai/accounts/<account-id>/handbook/<playbook-id>` |
| Playbook version URL | `https://app.wallfacer.ai/accounts/<account-id>/handbook/<playbook-id>/versions/<version>` |
| Handbook link | `wallfacer://handbook/pages/<page-id>` |

Rules that matter:

- **Names and paths resolve against active entries.** A deleted page or an archived playbook is reachable by ID only, which is what a restore needs.
- **Ambiguity is reported, never guessed.** A name matching two entries fails with every candidate's type, ID, and path. Paths are matched segment by segment and are case-insensitive.
- **`--type page` or `--type playbook` disambiguates.** Use it when a page and a playbook share a name, and to assert the type you expect: resolving the wrong type is an error, not a silent success.
- **A URL from another account is refused before any request is made.** The error names both accounts.

## Commands

```bash
wallfacer handbook tree
```

The handbook as one nested tree of pages and playbooks, exactly as the API returns it. Every node carries `type`, `id`, and its children, so any node reads with `handbook read <id>`. Archived playbooks and deleted pages are not in the tree.

```bash
wallfacer handbook list [--type page|playbook] [--page N] [--per-page N]
                        [--include-deleted] [--include-archived]
```

A flat list of entries with each one's path, parent, and state. `pagination` passes through the `meta` and `links` of each underlying endpoint, so `--page` traverses with the API's own numbers.

```bash
wallfacer handbook search <query> [--type ...] [--limit N] [--max-pages N]
```

Case-insensitive substring search over page titles, descriptions, and bodies, and over playbook names and descriptions. The account-wide search API route is feature-gated, so this walks the paginated page and pipeline reads instead; `pagination.<type>.pages_read` and `.complete` say how far the sweep got, and `truncated` says whether `--limit` cut it short.

```bash
wallfacer handbook read <reference> [--type page|playbook]
```

A page's record including its markdown body, or a playbook's record with `active_version` expanded to the full published definition (the pipeline endpoint returns only a summary).

An unpublished draft is never substituted for the active definition. `draft` in a playbook read reports `present`, `updated_at`, `updated_by`, and the command that returns the draft itself.

```bash
wallfacer handbook resolve <reference> [--type page|playbook]
```

Resolves without reading content: stable ID, type, account, path, and state. This is what a later write or run should use to turn a human-typed reference into an ID.

```bash
wallfacer handbook revisions <page-reference> [--page N] [--per-page N]
wallfacer handbook revision  <page-reference> <revision-id>
wallfacer handbook versions  <playbook-reference>
wallfacer handbook version   <playbook-reference> [version]
wallfacer handbook draft     <playbook-reference>
```

`version` takes a version number or a version UUID. Omit it to read the active version, or pass a playbook version URL as the reference and the version in it is used.

## Following a result

Every command returns a `follow_up` object naming the exact command for each reference in the result: the parent page, each child, each linked page of a playbook, its versions, its draft, a page's revisions. One result plus `--help` is enough to reach everything else.

`--query` projection and `-o yaml` work as they do everywhere else:

```bash
wallfacer handbook read "Engineering/Build" -q 'data.body' --raw
wallfacer handbook search "review" -q 'data[].{type: type, path: path, id: id}'
wallfacer handbook resolve "Writing Great PRs" -q 'data.id' --raw
```
