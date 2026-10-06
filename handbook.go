package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"slices"
	"sort"
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
onward. Page edits are snapshotted per editing session rather than per save, so the page's
revisions read back an earlier session's wording, not whatever the page said before your
last save.

Of the playbook authoring commands, only ` + "`create-playbook` and `publish`" + ` change a
playbook's versioned definition. Two histories run alongside each other and are not the
same thing. A page's **revisions** are its saved edits, and a page's current content reaches
every later run as soon as it is saved. A playbook's **versions** are its published
definitions: a task pins the version that was active when it was created and keeps running
that one, so publishing a new version changes later tasks and not the ones already in
flight. Linking or unlinking a page is metadata and reaches later runs immediately, without
a publish.

Output is text: pages as markdown with YAML frontmatter, playbook definitions as YAML that
` + "`save-draft`" + ` takes back, and lists one entry per line with the ID ` + "`read`" + ` takes.
A view of one record ends with ` + "`Next:`" + `, the commands for that record, labelled with
the entries they reach; a command is named only when the record has what it reads. Pass
` + "`-o json`" + ` for the structured payload (` + "`data`, `reference`, `follow_up`" + `) when
scripting; ` + "`-q`" + ` implies JSON.`),
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
	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Show the handbook as one nested tree of pages and playbooks",
		Long:  cli.Markdown("Prints the handbook as an outline: one line per page or playbook with its type, ID, and description, nested the way the tree is filed. Any line reads in full with `wallfacer handbook read <id>`. `--under` shows one branch and `--depth` limits how many levels print; an entry with hidden children says how many. Archived playbooks and deleted pages are not in the tree; reach those by ID.\n\nWith `-o json` the API's tree comes back unchanged (pruned to the same branch and depth)."),
		Example: `  wallfacer handbook tree
  wallfacer handbook tree --under "R&D/Engineering" --depth 1
  wallfacer handbook tree -o json`,
		Args: cobra.NoArgs,
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			under, _ := cmd.Flags().GetString("under")
			depth, _ := cmd.Flags().GetInt("depth")
			return runHandbookTree(api, under, depth)
		}),
	}
	cmd.Flags().String("under", "", "Show only the branch under this page (any page reference)")
	cmd.Flags().Int("depth", 0, "Levels to print (0 prints every level)")
	return cmd
}

func handbookListCommand(accountID string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List handbook entries as a flat, paginated list",
		Long:  cli.Markdown("Lists pages and playbooks as one flat list, one line per entry with its path, type, state, and ID. Pages and playbooks paginate separately; the last lines say where you are and print the command for the next page. With `-o json`, each endpoint's pagination metadata is under `pagination`."),
		Example: `  wallfacer handbook list --type playbook
  wallfacer handbook list --include-deleted --type page`,
		Args: cobra.NoArgs,
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
	cmd.Flags().Int("per-page", 0, "Results per page for each underlying endpoint (max 200)")
	cmd.Flags().Bool("include-deleted", false, "Include deleted pages")
	cmd.Flags().Bool("include-archived", false, "Include archived playbooks")
	return cmd
}

func handbookSearchCommand(accountID string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search pages and playbooks by title, description, and page body",
		Long:  cli.Markdown("Case-insensitive substring search across both entry types. Title matches list first, then description, then body; a body match shows the text around it. The API's account search route is feature-gated, so this walks the paginated page and pipeline reads instead, and says when it stopped before reading everything (`pagination` in `-o json`)."),
		Example: `  wallfacer handbook search "pull request"
  wallfacer handbook search triage --type playbook`,
		Args: cobra.ExactArgs(1),
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
		Long:  cli.Markdown("A page prints as a markdown document: YAML frontmatter (ID, path, parent, state), the title, and the body, followed by the commands for its parent, children, and revisions. `--body` prints the body alone, which round-trips through an editor: `read --body > page.md`, edit, `update --body-file page.md`. With `--body` the reference resolves as a page, so a name or path a page shares with a playbook reads the page.\n\nA playbook prints its triggers and its active version's steps with each step's settings and instructions. `wallfacer handbook version <playbook>` prints the same definition as editable YAML. An unpublished draft is never substituted for the active definition: the frontmatter says whether one is saved, and `wallfacer handbook draft` prints it.\n\nWith `-o json`, a page is the API record and a playbook carries `active_version` expanded to the full published definition."),
		Example: `  wallfacer handbook read "Writing Great PRs"
  wallfacer handbook read "R&D/Engineering/Build" --body > build.md
  wallfacer handbook read 019eaa1d-6a8e-72e7-83a3-99b055f6de75 -o json`,
		Args: cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			kind, err := handbookKindFlag(cmd)
			if err != nil {
				return err
			}
			if bodyOnly, _ := cmd.Flags().GetBool("body"); bodyOnly {
				return runHandbookReadBody(api, args[0], kind)
			}
			return runHandbookRead(api, args[0], kind)
		}),
	}
	addHandbookKindFlag(cmd)
	cmd.Flags().Bool("body", false, "Print only a page's markdown body, for editing and saving back with `handbook update --body-file`")
	return cmd
}

func handbookResolveCommand(accountID string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolve <reference>",
		Short: "Resolve a reference to one stable ID, type, account, and state",
		Long:  cli.Markdown("Resolves without reading content. Use it to turn a name, path, or detail URL into the stable ID a later write or run needs, and to see the entry's state before acting on it."),
		Example: `  wallfacer handbook resolve "R&D/Engineering/Build"
  wallfacer handbook resolve "Writing Great PRs" -o json -q data.id --raw`,
		Args: cobra.ExactArgs(1),
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
	cmd.Flags().Int("per-page", 0, "Results per page (max 100)")
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
	cmd := &cobra.Command{
		Use:   "versions <playbook-reference>",
		Short: "List a playbook's published versions, newest first",
		Args:  cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			page, _ := cmd.Flags().GetInt("page")
			perPage, _ := cmd.Flags().GetInt("per-page")
			return runHandbookVersions(api, args[0], page, perPage)
		}),
	}
	cmd.Flags().Int("page", 0, "Page of results to read (1-based; default is the first page)")
	cmd.Flags().Int("per-page", 0, "Results per page (max 200)")
	return cmd
}

func handbookVersionCommand(accountID string) *cobra.Command {
	return &cobra.Command{
		Use:   "version <playbook-reference> [version]",
		Short: "Read one published playbook version in full",
		Long:  cli.Markdown("The version is a version number or a version UUID. Omit it to read the active version, or pass a playbook version URL as the reference and the version in it is used.\n\nPrints the definition as a YAML document, with the version's details in comments, so the output is a starting draft: save it to a file, edit it, and pass it to `handbook save-draft --definition-file`."),
		Example: `  wallfacer handbook version "Research and Improve Ideas" > playbook.yaml
  wallfacer handbook version "Research and Improve Ideas" 3`,
		Args: cobra.RangeArgs(1, 2),
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

func runHandbookTree(api *handbookAPI, under string, depth int) error {
	idx, err := api.loadIndex()
	if err != nil {
		return err
	}

	if depth < 0 {
		return errors.New("--depth must be 0 (every level) or more")
	}

	nodes := idx.raw
	data := map[string]interface{}{}
	label := ""
	if under != "" {
		ref, err := api.resolveHandbookRef(under, kindPage)
		if err != nil {
			return err
		}
		node := findTreeNode(idx.raw, ref.ID)
		if node == nil {
			return errors.Errorf("page %s is not in the handbook tree", ref.ID)
		}
		nodes = asList(node["children"])
		data["under"] = ref
		label = ref.Path
	}
	data["tree"] = pruneTree(nodes, depth)

	return emitHandbookAs(map[string]interface{}{
		"data": data,
		"follow_up": map[string]interface{}{
			"read":    "wallfacer handbook read <id|name|path>",
			"list":    "wallfacer handbook list --type page|playbook",
			"search":  "wallfacer handbook search <query>",
			"resolve": "wallfacer handbook resolve <id|name|path|url>",
		},
	}, renderHandbookTree(label))
}

// findTreeNode returns the tree node with the given ID, at any depth.
func findTreeNode(nodes []interface{}, id string) map[string]interface{} {
	for _, item := range nodes {
		node := asMap(item)
		if node == nil {
			continue
		}
		if stringField(node, "id") == id {
			return node
		}
		if found := findTreeNode(asList(node["children"]), id); found != nil {
			return found
		}
	}
	return nil
}

// pruneTree copies the tree down to depth levels (0 keeps every level). A
// node whose children were cut keeps an empty children list and reports how
// many entries sit below it in children_hidden, so a shallow view still says
// where there is more.
func pruneTree(nodes []interface{}, depth int) []interface{} {
	if depth == 0 {
		return nodes
	}
	out := make([]interface{}, 0, len(nodes))
	for _, item := range nodes {
		node := asMap(item)
		if node == nil {
			out = append(out, item)
			continue
		}
		copied := map[string]interface{}{}
		for key, value := range node {
			copied[key] = value
		}
		children := asList(node["children"])
		if depth == 1 {
			if hidden := countEntries(children); hidden > 0 {
				copied["children_hidden"] = hidden
			}
			copied["children"] = []interface{}{}
		} else {
			copied["children"] = pruneTree(children, depth-1)
		}
		out = append(out, copied)
	}
	return out
}

func countEntries(nodes []interface{}) int {
	count := 0
	for _, item := range nodes {
		count += 1 + countEntries(asList(asMap(item)["children"]))
	}
	return count
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

	listCommand := func(next string) string {
		cmd := "wallfacer handbook list --page " + next
		if kind != "" {
			cmd += " --type " + kind
		}
		if perPage > 0 {
			cmd += fmt.Sprintf(" --per-page %d", perPage)
		}
		if includeDeleted {
			cmd += " --include-deleted"
		}
		if includeArchived {
			cmd += " --include-archived"
		}
		return cmd
	}

	return emitHandbookAs(map[string]interface{}{
		"data":       entries,
		"pagination": pagination,
		// next_page carries the same flags as the text view's next-page
		// command, with the page left for the caller.
		"follow_up": map[string]interface{}{
			"read":      "wallfacer handbook read <id>",
			"next_page": listCommand("<n>"),
		},
	}, renderHandbookList(func(next int) string { return listCommand(strconv.Itoa(next)) }))
}

func runHandbookSearch(api *handbookAPI, query, kind string, limit, maxPages int) error {
	idx, err := api.loadIndex()
	if err != nil {
		return err
	}
	if limit <= 0 {
		limit = 20
	}

	// Read every candidate (up to --max-pages per type), rank, then cut to
	// the limit: stopping at the first `limit` hits would return whatever the
	// API happened to list first, not the best matches.
	needle := strings.ToLower(query)
	matches := []*handbookRef{}
	pagination := map[string]interface{}{}

	if kind == "" || kind == kindPage {
		sweep, err := api.sweep(api.listPages, maxPages, func(record map[string]interface{}) bool {
			matchedIn := matchedFields(needle, map[string]string{
				"title":       stringField(record, "title"),
				"description": stringField(record, "description"),
				"body":        stringField(record, "body"),
			})
			if len(matchedIn) == 0 {
				return true
			}
			ref := withIndexedPath(idx, refFromPageRecord(record, api.accountID, "search"))
			ref.MatchedIn = matchedIn
			if slices.Contains(matchedIn, "body") {
				ref.Snippet = snippetAround(stringField(record, "body"), query, 60)
			}
			matches = append(matches, ref)
			return true
		})
		if err != nil {
			return err
		}
		pagination["pages"] = sweep
	}

	if kind == "" || kind == kindPlaybook {
		sweep, err := api.sweep(api.listPipelines, maxPages, func(record map[string]interface{}) bool {
			matchedIn := matchedFields(needle, map[string]string{
				"title":       stringField(record, "name"),
				"description": stringField(record, "description"),
			})
			if len(matchedIn) == 0 {
				return true
			}
			ref := withIndexedPath(idx, refFromPipelineRecord(record, api.accountID, "search"))
			ref.MatchedIn = matchedIn
			matches = append(matches, ref)
			return true
		})
		if err != nil {
			return err
		}
		pagination["playbooks"] = sweep
	}

	// Best match first: a title hit outranks a description hit, which
	// outranks a body hit. The order within each rank is the API's.
	sort.SliceStable(matches, func(i, j int) bool {
		return matchRank(matches[i]) < matchRank(matches[j])
	})
	total := len(matches)
	if total > limit {
		matches = matches[:limit]
	}

	// searchCommand repeats this search with one dimension widened, keeping
	// the query, type, and the other bound as they were.
	searchCommand := func(limit, maxPages int) string {
		cmd := "wallfacer handbook search " + shellQuote(query)
		if kind != "" {
			cmd += " --type " + kind
		}
		return cmd + fmt.Sprintf(" --limit %d --max-pages %d", limit, maxPages)
	}

	return emitHandbookAs(map[string]interface{}{
		"data":       matches,
		"query":      query,
		"limit":      limit,
		"total":      total,
		"truncated":  total > limit,
		"pagination": pagination,
		"follow_up": map[string]interface{}{
			"read":       "wallfacer handbook read <id>",
			"widen":      "wallfacer handbook search <query> --limit <n> --max-pages <n>",
			"whole_tree": "wallfacer handbook tree",
		},
	}, renderHandbookSearch(searchCommand, maxPages))
}

func matchRank(ref *handbookRef) int {
	if len(ref.MatchedIn) == 0 {
		return 3
	}
	switch ref.MatchedIn[0] {
	case "title":
		return 0
	case "description":
		return 1
	}
	return 2
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

	return emitHandbookAs(map[string]interface{}{
		"data":      record,
		"reference": ref,
		"follow_up": api.followUp(ref),
	}, renderHandbookRead(api))
}

// runHandbookReadBody prints a page's markdown body and nothing else, in every
// output format: it is the file an editor opens and `update --body-file`
// takes back. The reference resolves as a page, so a page and a playbook that
// share a name read the page; kind is the --type the caller passed, and a
// playbook is refused rather than ignored.
func runHandbookReadBody(api *handbookAPI, reference, kind string) error {
	if kind == kindPlaybook {
		return errors.New("--body reads a page's body and cannot be combined with --type playbook; `wallfacer handbook version <playbook>` prints a playbook's definition as editable YAML")
	}
	ref, err := api.resolveHandbookRef(reference, kindPage)
	if err != nil {
		// A reference that names only a playbook is pointed at the
		// playbook's editable form rather than just refused. An ambiguous
		// name is reported as it is: it names more than one page.
		if _, ambiguous := err.(*ambiguousRefError); ambiguous {
			return err
		}
		if playbook, playbookErr := api.resolveHandbookRef(reference, kindPlaybook); playbookErr == nil {
			return errors.Errorf("--body reads a page's body, and %q is a playbook; `wallfacer handbook version %s` prints a playbook's definition as editable YAML", reference, playbook.ID)
		}
		return err
	}
	record, err := api.readPage(ref)
	if err != nil {
		return handbookReadError(err, ref)
	}
	body := stringField(record, "body")
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	_, err = io.WriteString(cli.Stdout, body)
	return err
}

func runHandbookResolve(api *handbookAPI, reference, kind string) error {
	ref, err := api.resolveHandbookRef(reference, kind)
	if err != nil {
		return err
	}
	if _, err := api.loadIndex(); err != nil {
		return err
	}
	return emitHandbookAs(map[string]interface{}{
		"data":      ref,
		"follow_up": api.followUp(ref),
	}, renderHandbookResolve(api))
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

	items := responseList(resp)
	followUp := map[string]interface{}{
		"next_page": nextPageCommand(fmt.Sprintf("wallfacer handbook revisions %s", ref.ID), perPage),
	}
	// Name the newest revision on this page of results, so the command runs as
	// printed rather than leaving the caller to paste an id into it.
	if id := firstRevisionID(items); id != "" {
		followUp["revision"] = fmt.Sprintf("wallfacer handbook revision %s %s", ref.ID, id)
	}

	return emitHandbookAs(map[string]interface{}{
		"data":       items,
		"reference":  ref,
		"pagination": paginationOf(resp),
		"follow_up":  followUp,
	}, renderHandbookRevisions(api))
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

	return emitHandbookAs(map[string]interface{}{
		"data":      record,
		"reference": ref,
		"follow_up": map[string]interface{}{
			"revisions": fmt.Sprintf("wallfacer handbook revisions %s", ref.ID),
			"current":   fmt.Sprintf("wallfacer handbook read %s", ref.ID),
		},
	}, renderHandbookRevision(api))
}

func runHandbookVersions(api *handbookAPI, reference string, page, perPage int) error {
	ref, err := api.resolveHandbookRef(reference, kindPlaybook)
	if err != nil {
		return err
	}

	resp, err := api.listPipelineVersions(ref.ID, paginationQuery(page, perPage))
	if err != nil {
		return handbookReadError(err, ref)
	}

	items := responseList(resp)
	followUp := map[string]interface{}{
		"next_page": nextPageCommand(fmt.Sprintf("wallfacer handbook versions %s", ref.ID), perPage),
	}
	// With no version argument the command reads the active version, so it is
	// named only when there is one.
	if ref.ActiveVersion != nil {
		followUp["active"] = fmt.Sprintf("wallfacer handbook version %s", ref.ID)
	}
	// Same as revisions: name the newest version on this page of results rather
	// than printing a command with a placeholder still in it.
	if version := firstVersionArgument(items); version != "" {
		followUp["version"] = fmt.Sprintf("wallfacer handbook version %s %s", ref.ID, version)
	}

	return emitHandbookAs(map[string]interface{}{
		"data":       items,
		"reference":  ref,
		"pagination": paginationOf(resp),
		"follow_up":  followUp,
	}, renderHandbookVersions(api))
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

	return emitHandbookAs(map[string]interface{}{
		"data":      record,
		"reference": ref,
		"follow_up": map[string]interface{}{
			"versions": fmt.Sprintf("wallfacer handbook versions %s", ref.ID),
			"playbook": fmt.Sprintf("wallfacer handbook read %s", ref.ID),
		},
	}, renderHandbookVersion(api))
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

	// A tree node carries no version count; the record does, and it is what
	// tells "nothing published" from "published, none active".
	versionCount, _ := record["version_count"].(float64)
	followUp := map[string]interface{}{}
	switch {
	case ref.ActiveVersion != nil:
		followUp["active"] = fmt.Sprintf("wallfacer handbook version %s", ref.ID)
	case versionCount > 0:
		followUp["versions"] = fmt.Sprintf("wallfacer handbook versions %s", ref.ID)
	}

	return emitHandbookAs(map[string]interface{}{
		"data": map[string]interface{}{
			"present": present,
			"draft":   draft,
		},
		"reference": ref,
		"follow_up": followUp,
	}, renderHandbookDraft(api, int(versionCount)))
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

// nextPageCommand is the command for the following page of a listing, with
// `<n>` left for the renderer to fill. A page size the caller chose is carried
// over, since the next page under a different size skips or repeats records.
func nextPageCommand(base string, perPage int) string {
	cmd := base + " --page <n>"
	if perPage > 0 {
		cmd += fmt.Sprintf(" --per-page %d", perPage)
	}
	return cmd
}

// firstRevisionID takes the id of the newest revision in a revisions listing,
// which the endpoint returns most-recent first. Empty when the page has no
// revisions on it, in which case no `revision` command is named at all.
func firstRevisionID(items []interface{}) string {
	if len(items) == 0 {
		return ""
	}
	record, ok := items[0].(map[string]interface{})
	if !ok {
		return ""
	}
	return stringField(record, "id")
}

// firstVersionArgument is firstRevisionID for a playbook versions listing,
// where the path takes either the integer version number or the version's UUID.
func firstVersionArgument(items []interface{}) string {
	if len(items) == 0 {
		return ""
	}
	record, ok := items[0].(map[string]interface{})
	if !ok {
		return ""
	}
	if version := versionArgument(record["version"]); version != "" {
		return version
	}
	return versionArgument(record["id"])
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
		// No revision id is in hand, which is why the entry is `revisions`:
		// that listing is what names a concrete `revision` command.
		out["revisions"] = fmt.Sprintf("wallfacer handbook revisions %s", ref.ID)
	case kindPlaybook:
		// Each command is named only when the playbook has what it reads: with
		// no version argument `version` reads the active version, and an
		// unpublished playbook has none.
		if ref.hasVersions() {
			out["versions"] = fmt.Sprintf("wallfacer handbook versions %s", ref.ID)
		}
		if ref.ActiveVersion != nil {
			out["version"] = fmt.Sprintf("wallfacer handbook version %s", ref.ID)
		}
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

// sweep walks an offset-paginated list endpoint, handing every record to visit
// until it returns false or the pages run out, and reports how far it got.
// Every listing the CLI sweeps is offset-paginated except the agents listing;
// that one takes sweepCursor.
func (a *handbookAPI) sweep(list func(url.Values) (map[string]interface{}, error), maxPages int, visit func(map[string]interface{}) bool) (map[string]interface{}, error) {
	return a.sweepWith(&offsetPager{}, list, maxPages, visit)
}

// sweepCursor is sweep for a cursor-paginated endpoint. `GET /agents` is
// cursor-paginated, and discards a `page=N` it is sent: paged that way it
// answers with the first page forever, so the sweep never terminates.
func (a *handbookAPI) sweepCursor(list func(url.Values) (map[string]interface{}, error), maxPages int, visit func(map[string]interface{}) bool) (map[string]interface{}, error) {
	return a.sweepWith(&cursorPager{}, list, maxPages, visit)
}

func (a *handbookAPI) sweepWith(pages pager, list func(url.Values) (map[string]interface{}, error), maxPages int, visit func(map[string]interface{}) bool) (map[string]interface{}, error) {
	switch {
	case maxPages == sweepUnbounded:
		maxPages = math.MaxInt32
	case maxPages <= 0:
		maxPages = 20
	}

	scanned := 0
	complete := false
	var last map[string]interface{}

	for scanned < maxPages {
		query := url.Values{}
		query.Set("per_page", "100")
		pages.apply(query)

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

		if !pages.advance(resp) {
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

// pager carries a sweep from one request to the next. apply writes whatever
// identifies the page being asked for into the query, and advance reads the
// response just received, keeping what the next apply needs and reporting
// whether there is a next page at all.
type pager interface {
	apply(query url.Values)
	advance(resp map[string]interface{}) bool
}

type offsetPager struct {
	page int
}

func (p *offsetPager) apply(query url.Values) {
	if p.page > 1 {
		query.Set("page", strconv.Itoa(p.page))
	}
}

func (p *offsetPager) advance(resp map[string]interface{}) bool {
	if !hasNextPage(resp) {
		return false
	}
	if p.page == 0 {
		p.page = 1
	}
	p.page++
	return true
}

type cursorPager struct {
	cursor string
}

func (p *cursorPager) apply(query url.Values) {
	if p.cursor != "" {
		query.Set("cursor", p.cursor)
	}
}

func (p *cursorPager) advance(resp map[string]interface{}) bool {
	p.cursor = nextCursor(resp)
	return p.cursor != ""
}

// nextCursor reads the cursor for the following page out of a cursor-paginated
// response. Laravel reports it as `meta.next_cursor` and, redundantly, as the
// `cursor` query value of `links.next`; either is accepted so a response that
// carries only one of them still traverses.
func nextCursor(resp map[string]interface{}) string {
	if meta, ok := resp["meta"].(map[string]interface{}); ok {
		if cursor, ok := meta["next_cursor"].(string); ok && cursor != "" {
			return cursor
		}
	}

	links, ok := resp["links"].(map[string]interface{})
	if !ok {
		return ""
	}
	next, ok := links["next"].(string)
	if !ok || next == "" {
		return ""
	}
	parsed, err := url.Parse(next)
	if err != nil {
		return ""
	}
	return parsed.Query().Get("cursor")
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
