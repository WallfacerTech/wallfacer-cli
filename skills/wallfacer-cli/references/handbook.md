# Handbook discovery, reading, and editing

`wallfacer handbook` finds, reads, edits, and organizes the account's handbook: pages and playbooks in one surface. No command in the group ever creates a task.

`tree`, `list`, `search`, `read`, `resolve`, and the revision and version commands are reads. `create`, `update`, `delete`, `restore`, `move`, and `reorder` write, and a page write is live knowledge immediately: what agents running playbooks read from the next task onward.

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

## Editing a page

```bash
wallfacer handbook create [<title>] [--title T] [--body M | --body-file PATH]
                          [--under <page-reference> | --top-level] [--position N]
wallfacer handbook update <page-reference> [--title T] [--body M | --body-file PATH]
                          [--clear-body] [--under <page-reference> | --top-level] [--position N]
```

Both accept a JSON object on stdin, the same body the `pages` group takes, and layer the flags over it: a flag wins over the same field in a piped body.

- **A page write is live.** The new content is what agents read from the next task onward.
- **History is retained.** Each editing session is snapshotted. `follow_up.revisions` in the result names the command that reads the earlier wording back, and `follow_up.revision` the one that reads a single snapshot.
- **Fields left out are left alone.** `--clear-body` empties the body, which is not the same as leaving `--body` off.
- **A missing title is refused before the request.** So is an update with no field to change.

```bash
wallfacer handbook delete <page-reference>
wallfacer handbook restore <page-id>
```

Deleting keeps the page's revision history and does not delete what is filed under it: sub-pages and playbooks move up to the deleted page's parent, or to the top level when the deleted page was top-level. The result lists the children that moved (`data.reparented`) and where they went (`data.reparented_to`, null for the top level).

Restore is by ID: names and paths resolve against active entries only, so `wallfacer handbook list --include-deleted` is where a deleted page's ID comes from. Content and history come back intact; if the page's parent was deleted in the meantime it returns at the top level, and `data.parent_page_id` says where it landed.

## Organizing the tree

```bash
wallfacer handbook move <reference> (--under <page-reference> | --top-level) [--position N] [--type page|playbook]
wallfacer handbook reorder (--under <page-reference> | --top-level) <reference>...
```

`move` refiles one entry of either type. Leave `--position` off and the entry keeps the position it had. Moving a playbook changes only its parent and position: its definition is not saved, published, or discarded, its triggers are untouched, and no task is created.

`reorder` writes one parent's child order in a single atomic request, and positions are assigned from the order given. Pages and playbooks share one ordering under a parent, so the list is mixed and must be that parent's complete set of children. A list that omits a current sibling, repeats one, or names an entry filed elsewhere is rejected and nothing is written, naming the entry that is wrong. To bring in an entry from another parent, `move` it first.

Server validation is preserved rather than reimplemented: a parent cycle, a bad parent, or a stale child list comes back as a non-zero exit with the server's status and message, never as a success. A reference from another account is refused before any request goes out.

## Following a result

Every command returns a `follow_up` object naming the exact command for each reference in the result: the parent page, each child, each linked page of a playbook, its versions, its draft, a page's revisions. One result plus `--help` is enough to reach everything else. A write returns the updated resource plus the reads that show what it did, including the revision commands for the page it touched.

`--query` projection and `-o yaml` work as they do everywhere else:

```bash
wallfacer handbook read "Engineering/Build" -q 'data.body' --raw
wallfacer handbook search "review" -q 'data[].{type: type, path: path, id: id}'
wallfacer handbook resolve "Writing Great PRs" -q 'data.id' --raw
```
