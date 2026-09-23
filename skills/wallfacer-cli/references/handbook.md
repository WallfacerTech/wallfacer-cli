# Handbook discovery, reading, editing, and playbook authoring

`wallfacer handbook` finds, reads, edits, and organizes the account's handbook: pages and playbooks in one surface, and authors its playbooks. No command in the group ever creates a task.

`tree`, `list`, `search`, `read`, `resolve`, `revisions`, `revision`, `versions`, `version`, `draft`, `diff`, and `diff-draft` are reads. `create`, `update`, `delete`, `restore`, `move`, and `reorder` write, and a page write is live knowledge immediately: what agents running playbooks read from the next task onward. `create-playbook`, `update-playbook`, `archive-playbook`, `restore-playbook`, `save-draft`, `discard-draft`, and `publish` write too, and of those only `create-playbook` and `publish` change a playbook's versioned definition.

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

- **Names and paths resolve against active entries.** Every command takes the same reference forms, `restore` and `restore-playbook` included; a deleted page or an archived playbook is reachable by ID alone, because no name or path can resolve to one.
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
wallfacer handbook versions  <playbook-reference> [--page N] [--per-page N]
wallfacer handbook version   <playbook-reference> [version]
wallfacer handbook draft     <playbook-reference>
```

`revisions` lists one row per editing session rather than one per save: the granularity is the coalescing window, so the newest revision holds the current session's latest body and the one before it holds the previous session's. See "History is per editing session, not per edit" under Writing pages.

`version` takes a version number or a version UUID. Omit it to read the active version, or pass a playbook version URL as the reference and the version in it is used.

## Authoring a playbook

```bash
wallfacer handbook create-playbook --name <name> [--description ...] [--parent <page-ref>]
                                   [--link-page <page-ref>]... [--draft]
                                   [--definition-file <path>]
wallfacer handbook update-playbook <playbook-reference> [--name ...] [--description ... | --clear-description]
                                   [--enable | --disable]
                                   [--link-page <page-ref>]... [--clear-linked-pages]
wallfacer handbook archive-playbook <playbook-reference>
wallfacer handbook restore-playbook <playbook-id>
```

**Creation publishes.** The definition given to `create-playbook` is validated server-side, stored as version 1, and made the active version in the same call. There is no separate backend draft lifecycle to walk through first. `--draft` additionally saves the same definition as an unpublished draft.

**`update-playbook` is metadata only.** Name, description, linked pages, and the enabled state, all of which take effect immediately. Steps and triggers change only through `publish`. Moving the playbook in the tree and ordering it among siblings are hierarchy edits and are not in this command.

**A create suffixes a taken name; a rename refuses it.** `create-playbook --name` with a name another non-archived playbook holds lands as `<name> (2)`, so read the name that landed off the result. `update-playbook --name` onto a taken name is refused by the server and nothing in that update is applied, including the other fields it carried. The CLI runs no name check of its own, so retry with a different name rather than treating a refusal as partial.

**A restore comes back disabled.** `restore-playbook` takes the same reference forms as every other command, which in practice means an ID or detail URL, since names resolve against active entries only. Its triggers stay cleared until `update-playbook --enable`, and the server renames it with a numeric suffix if another playbook claimed its name. A playbook that is not archived is refused before the request, and so is `update-playbook --enable` on one that is still archived: restore it first.

## Drafts and publication

```bash
wallfacer handbook save-draft    <playbook-reference> [--definition-file <path>]
wallfacer handbook diff-draft    <playbook-reference>
wallfacer handbook diff          <playbook-reference> <a> <b>
wallfacer handbook publish       <playbook-reference> [--notes ...] [--activate=false]
wallfacer handbook discard-draft <playbook-reference>
```

Definitions are read from `--definition-file` (JSON or YAML) or from stdin. The bare definition, the API's `{"definition": ...}` envelope, and the output `handbook version` and `handbook draft` print are all accepted, so a definition read with `handbook version` can be piped straight back into a draft.

- **Saving is not publishing.** `save-draft` stores a working copy and changes nothing about how the playbook runs; the response reports `published: false` and the unchanged active version. There is one draft per playbook and saving again overwrites it. Drafts are stored verbatim and are not validated until publication.
- **`diff-draft` compares locally.** It reads the draft and the active version and compares them field by field, emitting one entry per differing path (`added`, `removed`, `changed`). It issues no writes: nothing is published to produce a comparison. With no published version yet, every field of the draft reads as added.
- **`diff` returns both definitions side by side**, for two published versions each named by version number or UUID. The API sends back each side's whole definition rather than a change list; `diff-draft` is the one that computes a change list.
- **`publish` operates on the saved draft.** It reads the draft back and sends it to the version endpoint. With no draft, with a stored draft that holds no definition object, or on a server validation failure, it fails and says no version was created. On success it returns the new version's identifiers, enough to read it with `handbook version`.
- **`--activate=false`** publishes without activating, and the result reports `published_version` and `active_version` separately with `activated: false`.
- **Publishing never enables a disabled playbook.** A disabled playbook is still disabled afterwards; the result says so and names `update-playbook --enable`.
- **`discard-draft`** uses the draft's own delete operation and leaves the published definition exactly as it was.

To submit a definition directly without saving it as a draft, the low-level `wallfacer versions create <playbook-id>` command still takes one.

## Editing a page

```bash
wallfacer handbook create [<title>] [--title T] [--body M | --body-file PATH]
                          [--under <page-reference> | --top-level] [--position N]
wallfacer handbook update <page-reference> [--title T] [--body M | --body-file PATH]
                          [--clear-body] [--under <page-reference> | --top-level] [--position N]
```

Both accept a JSON object on stdin, the same body the `pages` group takes, and layer the flags over it: a flag wins over the same field in a piped body. A body flag (`--body`, `--body-file`, `--clear-body`) means stdin is not read at all, so the command never blocks on a pipe that stays open; pipe the whole JSON object when you want other fields to come from it too.

- **A page write is live.** The new content is what agents read from the next task onward.
- **History is per editing session, not per edit.** Saves by the same author within ten minutes of that session's first save update the same revision in place, so the row holds the session's latest body; a different author, or a save after the window, starts a new revision. `follow_up.revisions` in the result names the command that lists them, and that listing names a concrete `revision` command for a single snapshot, but what they read back is the wording as of an earlier session. Recovering an intermediate wording from within a session you are still in is not possible.
- **Fields left out are left alone,** with one exception: an `update` that moves the page (`--under` or `--top-level`) and names no `--position` appends it to the end of its new parent. `--clear-body` empties the body, which is not the same as leaving `--body` off.
- **`--position` means different things on the two commands.** On `create` it is the position written, a sort key among the siblings with lower first. On `update` it is an insert point: the sibling there and everything after it shift down, the parent renormalizes to dense `0..n-1` positions, and a value past the last sibling appends, so the position in the result is the one the page landed on rather than the integer sent.
- **A missing title is refused before the write.** So is an update with no field to change. Both refusals happen client-side, so nothing is written; a reference that still needs resolving (`--under` on `create`, the page reference on `update`) may issue the normal reference-resolution GETs first.

```bash
wallfacer handbook delete <page-reference>
wallfacer handbook restore <page-reference>
```

Deleting keeps the page's revision history and does not delete what is filed under it: sub-pages and playbooks move up to the deleted page's parent, or to the top level when the deleted page was top-level. The result lists the children that moved (`data.reparented`) and where they went (`data.reparented_to`, null for the top level). With no children to move, `reparented` is empty and `reparented_to` is null rather than naming a page that received nothing.

Restore takes the same reference forms as every other command and is guarded by state: a page that is not deleted is refused. Names and paths resolve against active entries only, so in practice the target is an ID, and `wallfacer handbook list --include-deleted` is where a deleted page's ID comes from. Content and history come back intact; if the page's parent was deleted in the meantime it returns at the top level, and `data.parent_page_id` says where it landed.

## Organizing the tree

```bash
wallfacer handbook move <reference> (--under <page-reference> | --top-level) [--position N] [--type page|playbook]
wallfacer handbook reorder (--under <page-reference> | --top-level) <reference>...
```

`move` refiles one entry of either type. Leave `--position` off and the entry goes to the end of its new parent; a move whose destination is the parent it already has leaves its position alone. A negative `--position` is refused before the request, so a move is never reported for a field that was not sent. So is moving an archived playbook, which has no place in the tree until `restore-playbook` brings it back.

`--position` on `move` and on `update` is an insert point, not a sort key: the sibling holding that position and everything after it shift down, the parent renormalizes to dense `0..n-1` positions, and a value past the last sibling appends. The position the entry lands on is the server's, not necessarily the integer sent, so read it back from the result. On `create` it is still written verbatim (see "Editing a page" above). Moving a playbook changes only its parent and position: its definition is not saved, published, or discarded, its triggers are untouched, and no task is created.

`reorder` writes one parent's child order in a single atomic request, and positions are assigned from the order given. Pages and playbooks share one ordering under a parent, so the list is mixed and must be that parent's complete set of children. A list that omits a current sibling, repeats one, or names an entry filed elsewhere is rejected and nothing is written, naming the entry that is wrong. To bring in an entry from another parent, `move` it first.

Server validation is preserved rather than reimplemented: a parent cycle, a bad parent, or a stale child list comes back as a non-zero exit with the server's status and message, never as a success. A reference from another account is refused before any request goes out.

## Page revisions versus playbook versions

These are separate histories and only one of them needs a publish.

| | Reaches later runs when | History |
|---|---|---|
| Page content | Saved | `handbook revisions` / `handbook revision` |
| A playbook's linked-page set | Saved (metadata, via `update-playbook`) | — |
| A playbook's steps and triggers | Published | `handbook versions` / `handbook version` |

A task pins the playbook version that was active when it was created and keeps running that version, so publishing changes later tasks and not the ones already in flight. Page content is not pinned: a run reads pages as they are at the time it reads them.

## Following a result

Every command returns a `follow_up` object naming the next command. When the result names one record — a read, a resolve, a write, a `revisions` or `versions` listing — each entry names the exact command for a reference in it: the parent page, each child, each linked page of a playbook, its versions, its draft, a page's revisions. Those run as printed, with no placeholder left to fill in: that is why a read names `revisions` rather than a `revision` command it holds no id for, and why the `revisions` listing names a concrete `revision` command and `versions` a concrete `version` one.

`tree`, `list` and `search` name many records at once, so their entries carry a `<id>`, `<query>` or `<n>` placeholder you fill from the record you picked rather than an id the command is holding. `next_page` is a placeholder for the same reason: which page to ask for is the caller's choice. One result plus `--help` is enough to reach everything else. A write returns the updated resource plus the reads that show what it did, including the revisions command for the page it touched.

`--query` projection and `-o yaml` work as they do everywhere else:

```bash
wallfacer handbook read "Engineering/Build" -q 'data.body' --raw
wallfacer handbook search "review" -q 'data[].{type: type, path: path, id: id}'
wallfacer handbook resolve "Writing Great PRs" -q 'data.id' --raw
```
