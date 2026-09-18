package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/h2non/gentleman.v2"
)

// The write half of the handbook surface: page authoring, and the hierarchy
// both entry types share. Every command here goes through the same reference
// resolution the reads use, so a name, path, or detail URL reaches the same
// stable ID a write needs, and a reference from another account is refused
// before any request goes out.
//
// Playbook definitions are not touched from here. Moving a playbook sends its
// parent and position and nothing else: no draft save, no publish, no trigger
// change, and no task.

// send issues one write against the account-scoped API. Writes are the only
// place the handbook commands leave GET, so they all go through here: a 204
// comes back as an empty map, and any status at or past 400 is an error rather
// than a result, so a rejected write is never printed as a success.
func (a *handbookAPI) send(method, path string, payload map[string]interface{}) (map[string]interface{}, error) {
	server := viper.GetString("server")
	if server == "" {
		server = openapiServers()[viper.GetInt("server-index")]["url"]
	}

	var req *gentleman.Request
	switch method {
	case http.MethodPost:
		req = cli.Client.Post().URL(server + path)
	case http.MethodPatch:
		req = cli.Client.Patch().URL(server + path)
	case http.MethodDelete:
		req = cli.Client.Delete().URL(server + path)
	default:
		return nil, errors.Errorf("unsupported method %s", method)
	}

	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		req = req.AddHeader("Content-Type", "application/json").BodyString(string(encoded))
	}

	resp, err := req.Do()
	if err != nil {
		return nil, errors.Wrap(err, "Request failed")
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, errHandbookNotFound
	}
	if resp.StatusCode >= 400 {
		return nil, errors.Errorf("HTTP %d: %s", resp.StatusCode, resp.String())
	}

	if strings.TrimSpace(resp.String()) == "" {
		return map[string]interface{}{}, nil
	}

	var decoded map[string]interface{}
	if err := cli.UnmarshalResponse(resp, &decoded); err != nil {
		return nil, errors.Wrap(err, "Unmarshalling response failed")
	}
	return decoded, nil
}

func (a *handbookAPI) createPage(payload map[string]interface{}) (map[string]interface{}, error) {
	resp, err := a.send(http.MethodPost, a.accountPath("/pages"), payload)
	if err != nil {
		return nil, err
	}
	return responseObject(resp)
}

func (a *handbookAPI) updatePage(id string, payload map[string]interface{}) (map[string]interface{}, error) {
	resp, err := a.send(http.MethodPatch, a.accountPath("/pages/%s", url.PathEscape(id)), payload)
	if err != nil {
		return nil, err
	}
	return responseObject(resp)
}

func (a *handbookAPI) deletePage(id string) error {
	_, err := a.send(http.MethodDelete, a.accountPath("/pages/%s", url.PathEscape(id)), nil)
	return err
}

func (a *handbookAPI) updatePipeline(id string, payload map[string]interface{}) (map[string]interface{}, error) {
	resp, err := a.send(http.MethodPatch, a.accountPath("/pipelines/%s", url.PathEscape(id)), payload)
	if err != nil {
		return nil, err
	}
	return responseObject(resp)
}

// reorderHandbook writes one parent's child order in a single atomic request.
// The API returns the whole tree, so the caller sees the result of the write
// rather than the list it sent.
func (a *handbookAPI) reorderHandbook(payload map[string]interface{}) (map[string]interface{}, error) {
	resp, err := a.send(http.MethodPatch, a.accountPath("/handbook"), payload)
	if err != nil {
		return nil, err
	}
	return responseObject(resp)
}

// handbookEdit is one requested write: the JSON body the caller piped in on
// stdin with the value flags already layered over it, plus the parent the
// command was asked to file the entry under. The parent stays a reference
// until the command runs, because resolving it is a read.
type handbookEdit struct {
	body      map[string]interface{}
	parentRef string
	topLevel  bool
}

// applyParent turns `--under` into a parent ID and `--top-level` into an
// explicit null. Neither flag leaves the field out of the payload entirely, so
// an update that does not mention the parent never moves the entry.
func (a *handbookAPI) applyParent(edit handbookEdit, payload map[string]interface{}) error {
	switch {
	case edit.topLevel:
		payload["parent_page_id"] = nil
	case edit.parentRef != "":
		parent, err := a.resolveHandbookRef(edit.parentRef, kindPage)
		if err != nil {
			return err
		}
		payload["parent_page_id"] = parent.ID
	}
	return nil
}

func (e handbookEdit) payload() map[string]interface{} {
	payload := map[string]interface{}{}
	for key, value := range e.body {
		payload[key] = value
	}
	return payload
}

func registerHandbookEditCommands(accountID string, handbookCmd *cobra.Command) {
	handbookCmd.AddCommand(
		handbookCreateCommand(accountID),
		handbookUpdateCommand(accountID),
		handbookDeleteCommand(accountID),
		handbookRestoreCommand(accountID),
		handbookMoveCommand(accountID),
		handbookReorderCommand(accountID),
	)
}

// addHandbookParentFlags adds the two ways to name a destination parent. They
// are mutually exclusive: `--under` files the entry under a page, `--top-level`
// takes it out of every page.
func addHandbookParentFlags(cmd *cobra.Command) {
	cmd.Flags().String("under", "", "File under this page (any page reference)")
	cmd.Flags().Bool("top-level", false, "File at the top level of the handbook")
}

func handbookParentFlags(cmd *cobra.Command) (string, bool, error) {
	under, _ := cmd.Flags().GetString("under")
	topLevel, _ := cmd.Flags().GetBool("top-level")
	if under != "" && topLevel {
		return "", false, errors.New("--under and --top-level name two different destinations; pass one")
	}
	return under, topLevel, nil
}

// handbookEditFromFlags builds the requested write: the JSON body piped in on
// stdin first, then the value flags over the top, so a flag always wins over
// the same field in a piped body.
func handbookEditFromFlags(cmd *cobra.Command) (handbookEdit, error) {
	edit := handbookEdit{body: map[string]interface{}{}}

	raw, err := cli.GetBody("application/json", nil)
	if err != nil {
		return edit, errors.Wrap(err, "reading the request body")
	}
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &edit.body); err != nil {
			return edit, errors.Wrap(err, "the request body is not a JSON object")
		}
		// A literal `null` unmarshals into a nil map rather than failing, and
		// the flags below would panic writing into it.
		if edit.body == nil {
			return edit, errors.New("the request body is not a JSON object")
		}
	}

	if cmd.Flags().Changed("title") {
		title, _ := cmd.Flags().GetString("title")
		edit.body["title"] = title
	}
	if cmd.Flags().Changed("body") && cmd.Flags().Changed("body-file") {
		return edit, errors.New("--body and --body-file name two different bodies; pass one")
	}
	if cmd.Flags().Changed("body") {
		body, _ := cmd.Flags().GetString("body")
		edit.body["body"] = body
	}
	if cmd.Flags().Changed("body-file") {
		path, _ := cmd.Flags().GetString("body-file")
		contents, err := os.ReadFile(path)
		if err != nil {
			return edit, errors.Wrapf(err, "reading %s", path)
		}
		edit.body["body"] = string(contents)
	}
	if cmd.Flags().Changed("clear-body") {
		edit.body["body"] = nil
	}
	if cmd.Flags().Changed("position") {
		position, _ := cmd.Flags().GetInt("position")
		edit.body["position"] = position
	}

	parentRef, topLevel, err := handbookParentFlags(cmd)
	if err != nil {
		return edit, err
	}
	edit.parentRef, edit.topLevel = parentRef, topLevel
	return edit, nil
}

func addHandbookContentFlags(cmd *cobra.Command) {
	cmd.Flags().String("title", "", "Page title")
	cmd.Flags().String("body", "", "Markdown body")
	cmd.Flags().String("body-file", "", "Read the markdown body from a file")
	cmd.Flags().Int("position", 0, "Sort position among siblings (lower sorts first)")
	addHandbookParentFlags(cmd)
}

func handbookCreateCommand(accountID string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create [<title>]",
		Short: "Create a handbook page",
		Long: cli.Markdown(`Creates a page and returns the created record.

A page is live knowledge the moment it exists: agents running playbooks read it from then on.

The body comes from ` + "`--body`" + `, ` + "`--body-file`" + `, or a JSON object on stdin, and a
flag wins over the same field in a piped body. File the page with
` + "`--under <page-reference>`" + ` or leave it at the top level.`),
		Example: `  wallfacer handbook create "Writing Great PRs" --body-file pr.md --under "R&D/Engineering"
  echo '{"title":"Release","body":"..."}' | wallfacer handbook create`,
		Args: cobra.MaximumNArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			edit, err := handbookEditFromFlags(cmd)
			if err != nil {
				return err
			}
			if len(args) == 1 {
				edit.body["title"] = args[0]
			}
			return runHandbookCreate(api, edit)
		}),
	}
	addHandbookContentFlags(cmd)
	return cmd
}

func handbookUpdateCommand(accountID string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <page-reference>",
		Short: "Update a page's title, body, or placement",
		Long: cli.Markdown(`Updates a page and returns the updated record.

The change is live immediately: the page's new content is what agents read from the next
task onward. Nothing is lost, though. Each editing session is snapshotted, so the previous
wording stays readable with ` + "`wallfacer handbook revisions <page>`" + ` and
` + "`wallfacer handbook revision <page> <revision-id>`" + `; the result names both commands.

Fields left out are left alone. ` + "`--clear-body`" + ` empties the body, which is not the same
as leaving ` + "`--body`" + ` off.`),
		Example: `  wallfacer handbook update "Engineering/Build" --body-file build.md
  echo '{"title":"Build"}' | wallfacer handbook update <page-id>`,
		Args: cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			edit, err := handbookEditFromFlags(cmd)
			if err != nil {
				return err
			}
			return runHandbookUpdate(api, args[0], edit)
		}),
	}
	addHandbookContentFlags(cmd)
	cmd.Flags().Bool("clear-body", false, "Clear the page's body")
	return cmd
}

func handbookDeleteCommand(accountID string) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <page-reference>",
		Short: "Delete a page, keeping its history and its children",
		Long: cli.Markdown(`Deletes the page. Two things survive it:

- **Anything filed under it.** Sub-pages and playbooks are not deleted; they move up to the
  deleted page's parent, or to the top level when the deleted page was top-level.
- **Its revision history.** The content and every revision stay readable, which is what makes
  ` + "`wallfacer handbook restore <page-id>`" + ` a real restore.

The result names the children that moved and where they moved to.`),
		Args: cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			return runHandbookDelete(api, args[0])
		}),
	}
}

func handbookRestoreCommand(accountID string) *cobra.Command {
	return &cobra.Command{
		Use:   "restore <page-id>",
		Short: "Restore a deleted page by ID",
		Long: cli.Markdown(`Restores a deleted page: its content and revision history come back intact.

Names and paths resolve against active entries only, so a deleted page is reached by its
stable ID (or a page detail URL carrying it). ` + "`wallfacer handbook list --include-deleted`" + `
is where that ID comes from.

If the page's parent was deleted in the meantime the page comes back at the top level; the
returned record's ` + "`parent_page_id`" + ` says where it actually landed.`),
		Args: cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			return runHandbookRestore(api, args[0])
		}),
	}
}

func handbookMoveCommand(accountID string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "move <reference>",
		Short: "Move a page or playbook under another page, or to the top level",
		Long: cli.Markdown(`Refiles one entry. Pass ` + "`--under <page-reference>`" + ` to file it under a page, or
` + "`--top-level`" + ` to take it out of every page. ` + "`--position`" + ` sets its slot among its new
siblings; leave it off and the entry keeps the position it had.

Moving a playbook changes where it sits and nothing else: its definition is not saved,
published, or discarded, its triggers are untouched, and no task is created.

The server rejects a move that would put a page under itself or under one of its own
sub-pages, and a reference from another account never reaches a request.`),
		Example: `  wallfacer handbook move "Engineering/Build" --under "R&D" --position 0
  wallfacer handbook move <playbook-id> --top-level`,
		Args: cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			kind, err := handbookKindFlag(cmd)
			if err != nil {
				return err
			}
			under, topLevel, err := handbookParentFlags(cmd)
			if err != nil {
				return err
			}
			if under == "" && !topLevel {
				return errors.New("a destination is required: pass --under <page-reference> or --top-level")
			}
			position := -1
			if cmd.Flags().Changed("position") {
				position, _ = cmd.Flags().GetInt("position")
			}
			return runHandbookMove(api, args[0], kind, handbookEdit{parentRef: under, topLevel: topLevel}, position)
		}),
	}
	addHandbookParentFlags(cmd)
	addHandbookKindFlag(cmd)
	cmd.Flags().Int("position", 0, "Sort position among the new siblings (lower sorts first)")
	return cmd
}

func handbookReorderCommand(accountID string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reorder <reference>...",
		Short: "Reorder one parent's children, pages and playbooks together",
		Long: cli.Markdown(`Writes the order of one parent's children in a single atomic request. Name the parent
with ` + "`--under <page-reference>`" + ` or ` + "`--top-level`" + `, then list its children in the order
you want them, by any reference each one resolves from.

Pages and playbooks share one ordering under a parent, so the list is a mixed list and must
be the parent's complete set of children. A list that omits a current sibling, repeats one,
or names an entry filed elsewhere is rejected, and nothing is written: an ordering built
from a stale view of the tree fails loudly rather than dropping the entry it never saw into
an arbitrary slot. To bring in an entry from another parent, ` + "`wallfacer handbook move`" + ` it
first.`),
		Example: `  wallfacer handbook reorder --under "R&D/Engineering" "Build" <playbook-id> "Review"`,
		Args:    cobra.MinimumNArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			under, topLevel, err := handbookParentFlags(cmd)
			if err != nil {
				return err
			}
			if under == "" && !topLevel {
				return errors.New("a parent is required: pass --under <page-reference> or --top-level")
			}
			return runHandbookReorder(api, under, topLevel, args)
		}),
	}
	addHandbookParentFlags(cmd)
	return cmd
}

func runHandbookCreate(api *handbookAPI, edit handbookEdit) error {
	payload := edit.payload()
	if err := api.applyParent(edit, payload); err != nil {
		return err
	}
	if title, _ := payload["title"].(string); strings.TrimSpace(title) == "" {
		return errors.New("a title is required: pass it as the first argument, --title, or a title field in the request body")
	}

	record, err := api.createPage(payload)
	if err != nil {
		return err
	}

	ref := refFromPageRecord(record, api.accountID, "created")
	return emitHandbook(map[string]interface{}{
		"data":      record,
		"reference": ref,
		"note":      "The page is live handbook knowledge from now on: agents running playbooks read it as written. Every editing session is snapshotted, so earlier wording stays readable through the page's revisions.",
		"follow_up": handbookWriteFollowUp(ref),
	})
}

func runHandbookUpdate(api *handbookAPI, reference string, edit handbookEdit) error {
	ref, err := api.resolveHandbookRef(reference, kindPage)
	if err != nil {
		return err
	}

	payload := edit.payload()
	if err := api.applyParent(edit, payload); err != nil {
		return err
	}
	if len(payload) == 0 {
		return errors.Errorf("nothing to update on page %s: pass a field to change", ref.ID)
	}

	record, err := api.updatePage(ref.ID, payload)
	if err != nil {
		return handbookWriteError(err, ref)
	}

	updated := api.refreshWritten(ref, refFromPageRecord(record, api.accountID, ref.ResolvedFrom))
	return emitHandbook(map[string]interface{}{
		"data":      record,
		"reference": updated,
		"note":      "The page's new content is live immediately. The previous wording is retained as a revision; `follow_up.revisions` lists them.",
		"follow_up": handbookWriteFollowUp(updated),
	})
}

func runHandbookDelete(api *handbookAPI, reference string) error {
	ref, err := api.resolveHandbookRef(reference, kindPage)
	if err != nil {
		return err
	}
	if ref.State == "deleted" {
		return errors.Errorf("page %s is already deleted", ref.ID)
	}

	// Read the children before the write: after it they belong to another
	// parent, and naming them is the only way the result says what the
	// delete actually did to the tree.
	idx, err := api.loadIndex()
	if err != nil {
		return err
	}
	reparented := []map[string]interface{}{}
	for _, child := range idx.childrenOfParent(ref.ID) {
		reparented = append(reparented, map[string]interface{}{"type": child.Type, "id": child.ID, "title": child.Title})
	}

	if err := api.deletePage(ref.ID); err != nil {
		return handbookWriteError(err, ref)
	}

	var reparentedTo interface{}
	if ref.ParentPageID != "" {
		reparentedTo = ref.ParentPageID
	}

	return emitHandbook(map[string]interface{}{
		"data": map[string]interface{}{
			"id":                 ref.ID,
			"title":              ref.Title,
			"deleted":            true,
			"reparented":         reparented,
			"reparented_to":      reparentedTo,
			"revision_history":   "retained",
			"restorable_with_id": ref.ID,
		},
		"reference": ref,
		"note":      "Sub-pages and playbooks filed under this page were not deleted: they moved up to `reparented_to` (null means the top level). The page keeps its content and revision history and can be restored by ID.",
		"follow_up": map[string]interface{}{
			"restore":   fmt.Sprintf("wallfacer handbook restore %s", ref.ID),
			"revisions": fmt.Sprintf("wallfacer handbook revisions %s", ref.ID),
			"deleted":   "wallfacer handbook list --include-deleted",
		},
	})
}

func runHandbookRestore(api *handbookAPI, reference string) error {
	ref, err := api.resolveHandbookRef(reference, kindPage)
	if err != nil {
		return err
	}
	if ref.State != "deleted" {
		return errors.Errorf("page %s is not deleted, so there is nothing to restore", ref.ID)
	}

	record, err := api.updatePage(ref.ID, map[string]interface{}{"deleted": false})
	if err != nil {
		return handbookWriteError(err, ref)
	}

	restored := api.refreshWritten(ref, refFromPageRecord(record, api.accountID, ref.ResolvedFrom))
	return emitHandbook(map[string]interface{}{
		"data":      record,
		"reference": restored,
		"note":      "The page is back with its content and revision history intact. If its parent was deleted in the meantime it came back at the top level; `data.parent_page_id` says where it landed.",
		"follow_up": handbookWriteFollowUp(restored),
	})
}

func runHandbookMove(api *handbookAPI, reference, kind string, edit handbookEdit, position int) error {
	ref, err := api.resolveHandbookRef(reference, kind)
	if err != nil {
		return err
	}

	// Hierarchy only. A playbook's definition, triggers, and draft are not in
	// this payload, so a move cannot publish, discard, or run anything.
	payload := map[string]interface{}{}
	if err := api.applyParent(edit, payload); err != nil {
		return err
	}
	if position >= 0 {
		payload["position"] = position
	}

	var record map[string]interface{}
	var moved *handbookRef
	switch ref.Type {
	case kindPage:
		record, err = api.updatePage(ref.ID, payload)
		if err != nil {
			return handbookWriteError(err, ref)
		}
		moved = api.refreshWritten(ref, refFromPageRecord(record, api.accountID, ref.ResolvedFrom))
	case kindPlaybook:
		record, err = api.updatePipeline(ref.ID, payload)
		if err != nil {
			return handbookWriteError(err, ref)
		}
		moved = api.refreshWritten(ref, refFromPipelineRecord(record, api.accountID, ref.ResolvedFrom))
	default:
		return errors.Errorf("unsupported handbook type %q", ref.Type)
	}

	note := "Moved. Only the entry's parent and position changed."
	if ref.Type == kindPlaybook {
		note = "Moved. Only the playbook's parent and position changed: its definition, draft, and triggers are untouched and no task was created."
	}

	return emitHandbook(map[string]interface{}{
		"data":      record,
		"reference": moved,
		"note":      note,
		"follow_up": handbookWriteFollowUp(moved),
	})
}

func runHandbookReorder(api *handbookAPI, parentRef string, topLevel bool, childRefs []string) error {
	idx, err := api.loadIndex()
	if err != nil {
		return err
	}

	parentID := ""
	if !topLevel {
		parent, err := api.resolveHandbookRef(parentRef, kindPage)
		if err != nil {
			return err
		}
		parentID = parent.ID
	}

	children := []map[string]interface{}{}
	ordered := []*handbookRef{}
	seen := map[string]bool{}
	for _, reference := range childRefs {
		child, err := api.resolveHandbookRef(reference, "")
		if err != nil {
			return err
		}
		if seen[child.ID] {
			return errors.Errorf("%q names %s %s twice; each child appears once in the order", reference, child.Type, child.ID)
		}
		seen[child.ID] = true
		ordered = append(ordered, child)
		children = append(children, map[string]interface{}{"type": child.Type, "id": child.ID})
	}

	// The submitted list has to be exactly this parent's current children, so
	// check it against the tree we already read and say which entry is wrong
	// rather than sending a write the server will reject with a count.
	if err := checkReorderCoversParent(idx, parentID, ordered, seen); err != nil {
		return err
	}

	var parentPageID interface{}
	if parentID != "" {
		parentPageID = parentID
	}
	tree, err := api.reorderHandbook(map[string]interface{}{
		"parent_page_id": parentPageID,
		"children":       children,
	})
	if err != nil {
		return err
	}

	return emitHandbook(map[string]interface{}{
		"data":     tree,
		"parent":   parentPageID,
		"children": ordered,
		"note":     "Positions were assigned from the order given, in one atomic write. Pages and playbooks share one ordering under a parent.",
		"follow_up": map[string]interface{}{
			"tree": "wallfacer handbook tree",
			"move": "wallfacer handbook move <reference> --under <page-reference>",
		},
	})
}

// checkReorderCoversParent rejects a list that is not exactly the parent's
// current children: an entry filed somewhere else, or a sibling left out.
func checkReorderCoversParent(idx *handbookIndex, parentID string, ordered []*handbookRef, seen map[string]bool) error {
	current := idx.childrenOfParent(parentID)
	currentIDs := map[string]bool{}
	for _, child := range current {
		currentIDs[child.ID] = true
	}

	for _, child := range ordered {
		if !currentIDs[child.ID] {
			return errors.Errorf("%s %s (%s) is not a child of %s; move it there first", child.Type, child.ID, child.Title, describeReorderParent(parentID))
		}
	}

	missing := []string{}
	for _, child := range current {
		if !seen[child.ID] {
			missing = append(missing, fmt.Sprintf("%s %s (%s)", child.Type, child.ID, child.Title))
		}
	}
	if len(missing) > 0 {
		return errors.Errorf("the order must list every child of %s; missing: %s", describeReorderParent(parentID), strings.Join(missing, ", "))
	}
	return nil
}

func describeReorderParent(parentID string) string {
	if parentID == "" {
		return "the top level"
	}
	return "page " + parentID
}

// childrenOfParent reads one parent's children out of the tree, with the empty
// ID standing for the top level, which has no page to hang them off.
func (idx *handbookIndex) childrenOfParent(parentID string) []*handbookRef {
	if parentID != "" {
		return idx.childrenOf[parentID]
	}
	roots := []*handbookRef{}
	for _, entry := range idx.entries {
		if entry.ParentPageID == "" {
			roots = append(roots, entry)
		}
	}
	return roots
}

// refreshWritten takes the entry's current fields from the record the write
// returned and recomputes its path, which a move or a retitle has just
// invalidated. The cached tree predates the write, but a write only relocates
// or renames the entry itself, so its new ancestors' paths in that tree still
// hold. When the new path cannot be computed the field is left empty and
// omitted rather than reported stale.
func (a *handbookAPI) refreshWritten(resolved, refreshed *handbookRef) *handbookRef {
	ref := refreshRef(resolved, refreshed)
	ref.Path = ""

	if ref.Title == "" {
		return ref
	}
	if ref.ParentPageID == "" {
		ref.Path = ref.Title
		return ref
	}

	idx, err := a.loadIndex()
	if err != nil {
		return ref
	}
	if parent, ok := idx.byID[ref.ParentPageID]; ok {
		ref.Path = parent.Path + "/" + ref.Title
	}
	return ref
}

// handbookWriteFollowUp names the reads that show what a write did: the record
// itself, and for a page the revision history that now holds the previous
// wording.
func handbookWriteFollowUp(ref *handbookRef) map[string]interface{} {
	out := map[string]interface{}{
		"read": fmt.Sprintf("wallfacer handbook read %s", ref.ID),
		"tree": "wallfacer handbook tree",
	}
	switch ref.Type {
	case kindPage:
		out["revisions"] = fmt.Sprintf("wallfacer handbook revisions %s", ref.ID)
		out["revision"] = fmt.Sprintf("wallfacer handbook revision %s <revision-id>", ref.ID)
	case kindPlaybook:
		out["versions"] = fmt.Sprintf("wallfacer handbook versions %s", ref.ID)
	}
	if ref.ParentPageID != "" {
		out["parent"] = fmt.Sprintf("wallfacer handbook read %s", ref.ParentPageID)
	}
	return out
}

func handbookWriteError(err error, ref *handbookRef) error {
	if err == errHandbookNotFound {
		return errors.Errorf("%s %s is no longer writable in account %s", ref.Type, ref.ID, ref.AccountID)
	}
	return err
}
