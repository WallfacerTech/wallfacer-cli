package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// registerHandbookCommands adds the top-level `handbook` surface: one place to
// list, search, and read the account's pages and playbooks, to edit a page, and
// to organize both kinds of entry in the tree, following the references each
// result hands back. It works through the existing account-scoped handbook,
// page, and pipeline endpoints; the generated `accounts handbook`, `pages`, and
// `pipelines` groups keep working unchanged.
func registerHandbookCommands(accountID string) {
	handbookCmd := &cobra.Command{
		Use:   "handbook",
		Short: "Read, edit, organize, and author handbook pages and playbooks",
		Long: cli.Markdown(`Read, edit, and organize the account's handbook, and author its playbooks.

Commands that take a ` + "`<reference>`" + ` accept a stable ID, a unique entry name, a full path
through the tree (` + "`R&D/Engineering/Build`" + `), a Wallfacer page or playbook detail URL
inside the configured account, or a ` + "`wallfacer://handbook/pages/<id>`" + ` link. Names and
paths resolve against active entries and report every candidate rather than guessing when
more than one matches; a deleted page is reached by ID, which is what a restore needs.

` + "`tree`, `list`, `search`, `read`, `resolve`, `revisions`, `revision`, `versions`," + `
` + "`version`, `draft`, `diff` and `diff-draft` are reads. The rest write." + `

` + "`create`, `update`, `delete`, `restore`, `move`, and `reorder`" + ` act on the handbook
tree, and a page write is live knowledge immediately: what agents read from the next task
onward. Page edits are snapshotted, so the previous wording stays readable through the
page's revisions.

Of the playbook authoring commands, only ` + "`create-playbook` and `publish`" + ` change a
playbook's versioned definition. Two histories run alongside each other and are not the
same thing. A page's **revisions** are its saved edits, and a page's current content reaches
every later run as soon as it is saved. A playbook's **versions** are its published
definitions: a task pins the version that was active when it was created and keeps running
that one, so publishing a new version changes later tasks and not the ones already in
flight. Linking or unlinking a page is metadata and reaches later runs immediately, without
a publish.`),
	}

	handbookCmd.AddCommand(
		handbookTreeCommand(accountID),
		handbookListCommand(accountID),
		handbookSearchCommand(accountID),
		handbookReadCommand(accountID),
		handbookResolveCommand(accountID),
		handbookRevisionsCommand(accountID),
		handbookRevisionCommand(accountID),
		handbookVersionsCommand(accountID),
		handbookVersionCommand(accountID),
		handbookDraftCommand(accountID),
	)
	registerHandbookEditCommands(accountID, handbookCmd)
	handbookCmd.AddCommand(playbookAuthoringCommands(accountID)...)

	cli.Root.AddCommand(handbookCmd)
}

// handbookRun wires a command body to the account, matching the error handling
// of the other product commands: print to stderr and exit non-zero.
func handbookRun(accountID string, body func(api *handbookAPI, cmd *cobra.Command, args []string) error) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		api, err := newHandbookAPI(accountID)
		if err == nil {
			err = body(api, cmd, args)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}

func handbookTreeCommand(accountID string) *cobra.Command {
	return &cobra.Command{
		Use:   "tree",
		Short: "Show the handbook as one nested tree of pages and playbooks",
		Long:  cli.Markdown("Returns the account's handbook tree unchanged: each node carries its `type`, `id`, and children, so any node can be read with `wallfacer handbook read <id>`. Archived playbooks and deleted pages are not in the tree; reach those by ID."),
		Args:  cobra.NoArgs,
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			return runHandbookTree(api)
		}),
	}
}

func handbookListCommand(accountID string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List handbook entries as a flat, paginated list",
		Long:  cli.Markdown("Lists pages and playbooks as one flat list with each entry's path, parent, and state. Pagination metadata for each underlying endpoint is returned under `pagination`; use `--page` to read past the first page."),
		Args:  cobra.NoArgs,
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			kind, err := handbookKindFlag(cmd)
			if err != nil {
				return err
			}
			page, _ := cmd.Flags().GetInt("page")
			perPage, _ := cmd.Flags().GetInt("per-page")
			includeDeleted, _ := cmd.Flags().GetBool("include-deleted")
			includeArchived, _ := cmd.Flags().GetBool("include-archived")
			return runHandbookList(api, kind, page, perPage, includeDeleted, includeArchived)
		}),
	}
	addHandbookKindFlag(cmd)
	cmd.Flags().Int("page", 0, "Page of results to read (1-based; default is the first page)")
	cmd.Flags().Int("per-page", 0, "Results per page for each underlying endpoint")
	cmd.Flags().Bool("include-deleted", false, "Include deleted pages")
	cmd.Flags().Bool("include-archived", false, "Include archived playbooks")
	return cmd
}

func handbookSearchCommand(accountID string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search pages and playbooks by title, description, and page body",
		Long:  cli.Markdown("Case-insensitive substring search across both entry types. The API's account search route is feature-gated, so this walks the paginated page and pipeline reads instead; `pagination` reports how far it got and whether the sweep was complete."),
		Args:  cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			kind, err := handbookKindFlag(cmd)
			if err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			maxPages, _ := cmd.Flags().GetInt("max-pages")
			return runHandbookSearch(api, args[0], kind, limit, maxPages)
		}),
	}
	addHandbookKindFlag(cmd)
	cmd.Flags().Int("limit", 20, "Maximum matches to return")
	cmd.Flags().Int("max-pages", 20, "Maximum pages to read from each underlying endpoint")
	return cmd
}

func handbookReadCommand(accountID string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "read <reference>",
		Short: "Read a page's body or a playbook's full active definition",
		Long:  cli.Markdown("For a page, returns the record including its markdown body. For a playbook, returns the record with `active_version` expanded to the full published definition. An unpublished draft is never substituted for the active definition: `draft` reports only whether one exists, and `wallfacer handbook draft` returns its content."),
		Args:  cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			kind, err := handbookKindFlag(cmd)
			if err != nil {
				return err
			}
			return runHandbookRead(api, args[0], kind)
		}),
	}
	addHandbookKindFlag(cmd)
	return cmd
}

func handbookResolveCommand(accountID string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolve <reference>",
		Short: "Resolve a reference to one stable ID, type, account, and state",
		Long:  cli.Markdown("Resolves without reading content. Use it to turn a name, path, or detail URL into the stable ID a later write or run needs, and to see the entry's state before acting on it."),
		Args:  cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			kind, err := handbookKindFlag(cmd)
			if err != nil {
				return err
			}
			return runHandbookResolve(api, args[0], kind)
		}),
	}
	addHandbookKindFlag(cmd)
	return cmd
}

func handbookRevisionsCommand(accountID string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revisions <page-reference>",
		Short: "List a page's revisions, newest first",
		Args:  cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			page, _ := cmd.Flags().GetInt("page")
			perPage, _ := cmd.Flags().GetInt("per-page")
			return runHandbookRevisions(api, args[0], page, perPage)
		}),
	}
	cmd.Flags().Int("page", 0, "Page of results to read (1-based; default is the first page)")
	cmd.Flags().Int("per-page", 0, "Results per page")
	return cmd
}

func handbookRevisionCommand(accountID string) *cobra.Command {
	return &cobra.Command{
		Use:   "revision <page-reference> <revision-id>",
		Short: "Read one page revision, including its markdown body",
		Args:  cobra.ExactArgs(2),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			return runHandbookRevision(api, args[0], args[1])
		}),
	}
}

func handbookVersionsCommand(accountID string) *cobra.Command {
	return &cobra.Command{
		Use:   "versions <playbook-reference>",
		Short: "List a playbook's published versions, newest first",
		Args:  cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			return runHandbookVersions(api, args[0])
		}),
	}
}

func handbookVersionCommand(accountID string) *cobra.Command {
	return &cobra.Command{
		Use:   "version <playbook-reference> [version]",
		Short: "Read one published playbook version in full",
		Long:  cli.Markdown("The version is a version number or a version UUID. Omit it to read the active version, or pass a playbook version URL as the reference and the version in it is used."),
		Args:  cobra.RangeArgs(1, 2),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			requested := ""
			if len(args) == 2 {
				requested = args[1]
			}
			return runHandbookVersion(api, args[0], requested)
		}),
	}
}

func handbookDraftCommand(accountID string) *cobra.Command {
	return &cobra.Command{
		Use:   "draft <playbook-reference>",
		Short: "Read a playbook's unpublished draft, separately from its active definition",
		Args:  cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			return runHandbookDraft(api, args[0])
		}),
	}
}

func addHandbookKindFlag(cmd *cobra.Command) {
	cmd.Flags().String("type", "", "Restrict to one resource type: page or playbook")
}

func handbookKindFlag(cmd *cobra.Command) (string, error) {
	value, _ := cmd.Flags().GetString("type")
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", nil
	case kindPage, "pages":
		return kindPage, nil
	case kindPlaybook, "playbooks", "pipeline", "pipelines":
		return kindPlaybook, nil
	default:
		return "", errors.Errorf("--type must be page or playbook, got %q", value)
	}
}

func runHandbookTree(api *handbookAPI) error {
	idx, err := api.loadIndex()
	if err != nil {
		return err
	}

	return emitHandbook(map[string]interface{}{
		"data": map[string]interface{}{"tree": idx.raw},
		"follow_up": map[string]interface{}{
			"read":    "wallfacer handbook read <id|name|path>",
			"list":    "wallfacer handbook list --type page|playbook",
			"search":  "wallfacer handbook search <query>",
			"resolve": "wallfacer handbook resolve <id|name|path|url>",
		},
	})
}

func runHandbookList(api *handbookAPI, kind string, page, perPage int, includeDeleted, includeArchived bool) error {
	idx, err := api.loadIndex()
	if err != nil {
		return err
	}

	entries := []*handbookRef{}
	pagination := map[string]interface{}{}

	if kind == "" || kind == kindPage {
		query := paginationQuery(page, perPage)
		if includeDeleted {
			query.Set("include_deleted", "true")
		}
		resp, err := api.listPages(query)
		if err != nil {
			return err
		}
		for _, item := range responseList(resp) {
			record, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			entries = append(entries, withIndexedPath(idx, refFromPageRecord(record, api.accountID, "list")))
		}
		pagination["pages"] = paginationOf(resp)
	}

	if kind == "" || kind == kindPlaybook {
		query := paginationQuery(page, perPage)
		if includeArchived {
			query.Set("include_archived", "true")
		}
		resp, err := api.listPipelines(query)
		if err != nil {
			return err
		}
		for _, item := range responseList(resp) {
			record, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			entries = append(entries, withIndexedPath(idx, refFromPipelineRecord(record, api.accountID, "list")))
		}
		pagination["playbooks"] = paginationOf(resp)
	}

	return emitHandbook(map[string]interface{}{
		"data":       entries,
		"pagination": pagination,
		"follow_up": map[string]interface{}{
			"read":      "wallfacer handbook read <id>",
			"next_page": "wallfacer handbook list --page <n>",
		},
	})
}

func runHandbookSearch(api *handbookAPI, query, kind string, limit, maxPages int) error {
	idx, err := api.loadIndex()
	if err != nil {
		return err
	}
	if limit <= 0 {
		limit = 20
	}

	needle := strings.ToLower(query)
	matches := []*handbookRef{}
	pagination := map[string]interface{}{}

	if (kind == "" || kind == kindPage) && len(matches) < limit {
		sweep, err := api.sweep(api.listPages, maxPages, func(record map[string]interface{}) bool {
			if len(matches) >= limit {
				return false
			}
			matchedIn := matchedFields(needle, map[string]string{
				"title":       stringField(record, "title"),
				"description": stringField(record, "description"),
				"body":        stringField(record, "body"),
			})
			if len(matchedIn) == 0 {
				return len(matches) < limit
			}
			ref := withIndexedPath(idx, refFromPageRecord(record, api.accountID, "search"))
			ref.MatchedIn = matchedIn
			matches = append(matches, ref)
			return len(matches) < limit
		})
		if err != nil {
			return err
		}
		pagination["pages"] = sweep
	}

	if (kind == "" || kind == kindPlaybook) && len(matches) < limit {
		sweep, err := api.sweep(api.listPipelines, maxPages, func(record map[string]interface{}) bool {
			if len(matches) >= limit {
				return false
			}
			matchedIn := matchedFields(needle, map[string]string{
				"title":       stringField(record, "name"),
				"description": stringField(record, "description"),
			})
			if len(matchedIn) == 0 {
				return len(matches) < limit
			}
			ref := withIndexedPath(idx, refFromPipelineRecord(record, api.accountID, "search"))
			ref.MatchedIn = matchedIn
			matches = append(matches, ref)
			return len(matches) < limit
		})
		if err != nil {
			return err
		}
		pagination["playbooks"] = sweep
	}

	return emitHandbook(map[string]interface{}{
		"data":       matches,
		"query":      query,
		"limit":      limit,
		"truncated":  len(matches) >= limit,
		"pagination": pagination,
		"follow_up": map[string]interface{}{
			"read":       "wallfacer handbook read <id>",
			"widen":      "wallfacer handbook search <query> --limit <n> --max-pages <n>",
			"whole_tree": "wallfacer handbook tree",
		},
	})
}

func runHandbookRead(api *handbookAPI, reference, kind string) error {
	ref, err := api.resolveHandbookRef(reference, kind)
	if err != nil {
		return err
	}

	var record map[string]interface{}
	switch ref.Type {
	case kindPage:
		record, err = api.readPage(ref)
		if err != nil {
			return handbookReadError(err, ref)
		}
		ref = refreshRef(ref, refFromPageRecord(record, api.accountID, ref.ResolvedFrom))
	case kindPlaybook:
		record, err = api.getPipeline(ref.ID)
		if err != nil {
			return handbookReadError(err, ref)
		}
		// Take the reference from the record rather than the tree node: it is
		// what carries the linked pages, version count, and archived state the
		// follow-up commands are built from.
		ref = refreshRef(ref, refFromPipelineRecord(record, api.accountID, ref.ResolvedFrom))
		if err := expandActiveVersion(api, record, ref.ID); err != nil {
			return err
		}
		// A draft is an unpublished working copy. Report that it exists and
		// where to read it; never fold it into the active definition.
		record["draft"] = draftSummary(record["draft"], ref.ID)
	default:
		return errors.Errorf("unsupported handbook type %q", ref.Type)
	}

	return emitHandbook(map[string]interface{}{
		"data":      record,
		"reference": ref,
		"follow_up": api.followUp(ref),
	})
}

func runHandbookResolve(api *handbookAPI, reference, kind string) error {
	ref, err := api.resolveHandbookRef(reference, kind)
	if err != nil {
		return err
	}
	return emitHandbook(map[string]interface{}{
		"data":      ref,
		"follow_up": api.followUp(ref),
	})
}

func runHandbookRevisions(api *handbookAPI, reference string, page, perPage int) error {
	ref, err := api.resolveHandbookRef(reference, kindPage)
	if err != nil {
		return err
	}

	resp, err := api.listPageRevisions(ref.ID, paginationQuery(page, perPage))
	if err != nil {
		return handbookReadError(err, ref)
	}

	return emitHandbook(map[string]interface{}{
		"data":       responseList(resp),
		"reference":  ref,
		"pagination": paginationOf(resp),
		"follow_up": map[string]interface{}{
			"revision":  fmt.Sprintf("wallfacer handbook revision %s <revision-id>", ref.ID),
			"next_page": fmt.Sprintf("wallfacer handbook revisions %s --page <n>", ref.ID),
		},
	})
}

func runHandbookRevision(api *handbookAPI, reference, revisionID string) error {
	ref, err := api.resolveHandbookRef(reference, kindPage)
	if err != nil {
		return err
	}

	record, err := api.getPageRevision(ref.ID, revisionID)
	if err != nil {
		if err == errHandbookNotFound {
			return errors.Errorf("page %s has no revision %s", ref.ID, revisionID)
		}
		return err
	}

	return emitHandbook(map[string]interface{}{
		"data":      record,
		"reference": ref,
		"follow_up": map[string]interface{}{
			"revisions": fmt.Sprintf("wallfacer handbook revisions %s", ref.ID),
			"current":   fmt.Sprintf("wallfacer handbook read %s", ref.ID),
		},
	})
}

func runHandbookVersions(api *handbookAPI, reference string) error {
	ref, err := api.resolveHandbookRef(reference, kindPlaybook)
	if err != nil {
		return err
	}

	resp, err := api.listPipelineVersions(ref.ID)
	if err != nil {
		return handbookReadError(err, ref)
	}

	return emitHandbook(map[string]interface{}{
		"data":       responseList(resp),
		"reference":  ref,
		"pagination": paginationOf(resp),
		"follow_up": map[string]interface{}{
			"version": fmt.Sprintf("wallfacer handbook version %s <version>", ref.ID),
			"active":  fmt.Sprintf("wallfacer handbook version %s", ref.ID),
		},
	})
}

func runHandbookVersion(api *handbookAPI, reference, requested string) error {
	ref, err := api.resolveHandbookRef(reference, kindPlaybook)
	if err != nil {
		return err
	}

	version := requested
	if version == "" {
		version = ref.versionHint
	}
	if version == "" {
		if ref.ActiveVersion == nil {
			return errors.Errorf("playbook %s has no active version; pass a version explicitly", ref.ID)
		}
		version = versionArgument(ref.ActiveVersion["version"])
	}
	if version == "" {
		return errors.Errorf("could not determine which version of playbook %s to read", ref.ID)
	}

	record, err := api.getPipelineVersion(ref.ID, version)
	if err != nil {
		if err == errHandbookNotFound {
			return errors.Errorf("playbook %s has no version %s", ref.ID, version)
		}
		return err
	}

	return emitHandbook(map[string]interface{}{
		"data":      record,
		"reference": ref,
		"follow_up": map[string]interface{}{
			"versions": fmt.Sprintf("wallfacer handbook versions %s", ref.ID),
			"playbook": fmt.Sprintf("wallfacer handbook read %s", ref.ID),
		},
	})
}

func runHandbookDraft(api *handbookAPI, reference string) error {
	ref, err := api.resolveHandbookRef(reference, kindPlaybook)
	if err != nil {
		return err
	}

	record, err := api.getPipeline(ref.ID)
	if err != nil {
		return handbookReadError(err, ref)
	}

	draft, present := record["draft"].(map[string]interface{})
	if !present {
		draft = nil
	}

	return emitHandbook(map[string]interface{}{
		"data": map[string]interface{}{
			"present": present,
			"draft":   draft,
		},
		"reference": ref,
		"follow_up": map[string]interface{}{
			"active": fmt.Sprintf("wallfacer handbook version %s", ref.ID),
		},
	})
}

// expandActiveVersion replaces the pipeline record's summary of its active
// version with the published definition itself, which the pipeline endpoint
// does not include.
func expandActiveVersion(api *handbookAPI, record map[string]interface{}, pipelineID string) error {
	active, ok := record["active_version"].(map[string]interface{})
	if !ok {
		return nil
	}

	version := versionArgument(active["version"])
	if version == "" {
		version = versionArgument(active["id"])
	}
	if version == "" {
		return nil
	}

	full, err := api.getPipelineVersion(pipelineID, version)
	if err != nil {
		if err == errHandbookNotFound {
			return nil
		}
		return err
	}
	record["active_version"] = full
	return nil
}

func draftSummary(draft interface{}, pipelineID string) map[string]interface{} {
	summary := map[string]interface{}{
		"present": false,
		"command": fmt.Sprintf("wallfacer handbook draft %s", pipelineID),
	}
	record, ok := draft.(map[string]interface{})
	if !ok {
		return summary
	}
	summary["present"] = true
	summary["updated_at"] = record["updated_at"]
	summary["updated_by"] = record["updated_by"]
	return summary
}

// followUp names the command for each reference the entry hands back, so the
// next read is available from one result plus `--help`.
func (a *handbookAPI) followUp(ref *handbookRef) map[string]interface{} {
	out := map[string]interface{}{
		"read": fmt.Sprintf("wallfacer handbook read %s", ref.ID),
	}

	switch ref.Type {
	case kindPage:
		out["revisions"] = fmt.Sprintf("wallfacer handbook revisions %s", ref.ID)
		out["revision"] = fmt.Sprintf("wallfacer handbook revision %s <revision-id>", ref.ID)
	case kindPlaybook:
		out["versions"] = fmt.Sprintf("wallfacer handbook versions %s", ref.ID)
		out["version"] = fmt.Sprintf("wallfacer handbook version %s <version>", ref.ID)
		if ref.HasDraft != nil && *ref.HasDraft {
			out["draft"] = fmt.Sprintf("wallfacer handbook draft %s", ref.ID)
		}
		if len(ref.LinkedPageIDs) > 0 {
			linked := []string{}
			for _, id := range ref.LinkedPageIDs {
				if pageID, ok := id.(string); ok {
					linked = append(linked, fmt.Sprintf("wallfacer handbook read %s", pageID))
				}
			}
			out["linked_pages"] = linked
		}
	}

	if ref.ParentPageID != "" {
		out["parent"] = fmt.Sprintf("wallfacer handbook read %s", ref.ParentPageID)
	}
	if a.index != nil {
		children := []string{}
		for _, child := range a.index.childrenOf[ref.ID] {
			children = append(children, fmt.Sprintf("wallfacer handbook read %s", child.ID))
		}
		if len(children) > 0 {
			out["children"] = children
		}
	}

	return out
}

// sweepUnbounded asks sweep to keep reading until the pages run out, for a
// lookup that has to be exhaustive rather than fast.
const sweepUnbounded = -1

// sweep walks a paginated list endpoint, handing every record to visit until it
// returns false or the pages run out, and reports how far it got.
func (a *handbookAPI) sweep(list func(url.Values) (map[string]interface{}, error), maxPages int, visit func(map[string]interface{}) bool) (map[string]interface{}, error) {
	switch {
	case maxPages == sweepUnbounded:
		maxPages = math.MaxInt32
	case maxPages <= 0:
		maxPages = 20
	}

	scanned := 0
	complete := false
	var last map[string]interface{}

	for page := 1; page <= maxPages; page++ {
		query := url.Values{}
		query.Set("per_page", "100")
		if page > 1 {
			query.Set("page", strconv.Itoa(page))
		}

		resp, err := list(query)
		if err != nil {
			return nil, err
		}
		last = resp
		scanned++

		keepGoing := true
		for _, item := range responseList(resp) {
			record, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if !visit(record) {
				keepGoing = false
				break
			}
		}

		if !hasNextPage(resp) {
			complete = true
			break
		}
		if !keepGoing {
			break
		}
	}

	sweep := map[string]interface{}{
		"pages_read": scanned,
		"complete":   complete,
	}
	if last != nil {
		if pagination := paginationOf(last); pagination != nil {
			sweep["meta"] = pagination["meta"]
			sweep["links"] = pagination["links"]
		}
	}
	return sweep, nil
}

func hasNextPage(resp map[string]interface{}) bool {
	links, ok := resp["links"].(map[string]interface{})
	if !ok {
		return false
	}
	next, ok := links["next"].(string)
	return ok && next != ""
}

// paginationOf passes the endpoint's own pagination contract through untouched,
// so the caller can traverse with the same numbers the API reported.
func paginationOf(resp map[string]interface{}) map[string]interface{} {
	pagination := map[string]interface{}{}
	if meta, ok := resp["meta"]; ok {
		pagination["meta"] = meta
	}
	if links, ok := resp["links"]; ok {
		pagination["links"] = links
	}
	if len(pagination) == 0 {
		return nil
	}
	return pagination
}

func paginationQuery(page, perPage int) url.Values {
	query := url.Values{}
	if page > 0 {
		query.Set("page", strconv.Itoa(page))
	}
	if perPage > 0 {
		query.Set("per_page", strconv.Itoa(perPage))
	}
	return query
}

func matchedFields(needle string, fields map[string]string) []string {
	matched := []string{}
	for _, name := range []string{"title", "description", "body"} {
		value, ok := fields[name]
		if !ok || value == "" {
			continue
		}
		if strings.Contains(strings.ToLower(value), needle) {
			matched = append(matched, name)
		}
	}
	if len(matched) == 0 {
		return nil
	}
	return matched
}

// refreshRef keeps how a reference was resolved while taking the entry's
// current fields from the record that was just read.
func refreshRef(resolved, refreshed *handbookRef) *handbookRef {
	refreshed.Path = resolved.Path
	refreshed.ResolvedFrom = resolved.ResolvedFrom
	refreshed.versionHint = resolved.versionHint
	return refreshed
}

func withIndexedPath(idx *handbookIndex, ref *handbookRef) *handbookRef {
	if indexed, ok := idx.byID[ref.ID]; ok {
		ref.Path = indexed.Path
	}
	return ref
}

func handbookReadError(err error, ref *handbookRef) error {
	if err == errHandbookNotFound {
		return errors.Errorf("%s %s is no longer readable in account %s", ref.Type, ref.ID, ref.AccountID)
	}
	return err
}

// emitHandbook prints a payload through the shared formatter. Payloads are
// round-tripped through JSON first so that `--query` projection and `-o yaml`
// see plain maps and slices, exactly as they do for generated commands.
func emitHandbook(payload interface{}) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var decoded interface{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return err
	}
	return cli.Formatter.Format(decoded)
}
