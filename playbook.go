package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

// playbookAuthoringCommands are the write half of the handbook surface: create
// a playbook, change the metadata that takes effect immediately, and move a
// definition through draft, comparison, and publication.
//
// The split these commands keep is the one the API keeps. A draft is a stored
// working copy and changes nothing about how the playbook runs; only a publish
// writes a new version, and only an active version is what a new task pins to.
func playbookAuthoringCommands(accountID string) []*cobra.Command {
	return []*cobra.Command{
		playbookCreateCommand(accountID),
		playbookUpdateCommand(accountID),
		playbookArchiveCommand(accountID),
		playbookRestoreCommand(accountID),
		playbookSaveDraftCommand(accountID),
		playbookDiffDraftCommand(accountID),
		playbookDiscardDraftCommand(accountID),
		playbookPublishCommand(accountID),
		playbookDiffCommand(accountID),
	}
}

func playbookCreateCommand(accountID string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create-playbook",
		Short: "Create a playbook from a full definition",
		Long: cli.Markdown(`Creates a playbook from a full definition read from ` + "`--definition-file`" + ` or stdin.

Creation publishes: the API validates the definition, stores it as version 1, and makes
it the playbook's active version in the same call. There is no separate draft-first
lifecycle to step through, and nothing else has to be run to make the new playbook live.
` + "`--draft`" + ` saves the definition as an unpublished draft immediately after creating
the playbook, for a playbook whose first published version is a placeholder.

Filing the playbook under a page is available here as ` + "`--parent`" + `; moving it
afterwards, and ordering it among its siblings, belong to the handbook organization
commands.`),
		Args: cobra.NoArgs,
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			definition, err := definitionFromFlags(cmd)
			if err != nil {
				return err
			}
			name, _ := cmd.Flags().GetString("name")
			if strings.TrimSpace(name) == "" {
				return errors.New("--name is required")
			}
			description, _ := cmd.Flags().GetString("description")
			parent, _ := cmd.Flags().GetString("parent")
			linked, _ := cmd.Flags().GetStringArray("link-page")
			draft, _ := cmd.Flags().GetBool("draft")
			return runPlaybookCreate(api, name, description, parent, linked, definition, draft)
		}),
	}
	addDefinitionFlags(cmd)
	cmd.Flags().String("name", "", "Display name, unique among the account's non-archived playbooks")
	cmd.Flags().String("description", "", "Human-readable description")
	cmd.Flags().String("parent", "", "Handbook page to file the playbook under (ID, name, path, or URL)")
	cmd.Flags().StringArray("link-page", nil, "Handbook page delivered on every run, repeatable (ID, name, path, or URL)")
	cmd.Flags().Bool("draft", false, "Also save the same definition as an unpublished draft")
	return cmd
}

func playbookUpdateCommand(accountID string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-playbook <playbook-reference>",
		Short: "Change a playbook's name, description, linked pages, or enabled state",
		Long: cli.Markdown(`Updates the metadata that takes effect immediately, and nothing else.

Steps and triggers are the versioned definition and change only through
` + "`handbook publish`" + `. Linked pages are metadata: adding or removing one changes
what later runs receive without publishing a version, and does not touch the definition.

` + "`--disable`" + ` clears the playbook's triggers so it spawns no new tasks while staying
visible and editable; ` + "`--enable`" + ` puts them back. Tasks already running continue
either way. Moving the playbook in the tree is not here: that is a hierarchy edit.`),
		Args: cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			update, err := playbookMetadataUpdate(cmd)
			if err != nil {
				return err
			}
			return runPlaybookUpdate(api, args[0], update)
		}),
	}
	cmd.Flags().String("name", "", "New display name")
	cmd.Flags().String("description", "", "New description")
	cmd.Flags().Bool("clear-description", false, "Clear the description")
	cmd.Flags().Bool("disable", false, "Stop the playbook spawning new tasks")
	cmd.Flags().Bool("enable", false, "Resume spawning tasks from the playbook's triggers")
	cmd.Flags().StringArray("link-page", nil, "Replace the linked pages with these, repeatable (ID, name, path, or URL)")
	cmd.Flags().Bool("clear-linked-pages", false, "Remove every linked page")
	return cmd
}

func playbookArchiveCommand(accountID string) *cobra.Command {
	return &cobra.Command{
		Use:   "archive-playbook <playbook-reference>",
		Short: "Archive a playbook, freeing its name",
		Long:  cli.Markdown("Soft-archives the playbook: it leaves the default list and its triggers are cleared, while the tasks already run against it keep their history and their pinned versions. Restore it by ID with `handbook restore-playbook`."),
		Args:  cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			return runPlaybookArchive(api, args[0])
		}),
	}
}

func playbookRestoreCommand(accountID string) *cobra.Command {
	return &cobra.Command{
		Use:   "restore-playbook <playbook-id>",
		Short: "Restore an archived playbook by ID",
		Long:  cli.Markdown("Takes the ID (or detail URL) of an archived playbook: names resolve against active entries only, so an archived playbook is reachable by ID alone. It comes back disabled, with its triggers still cleared until `handbook update-playbook --enable`, and renamed with a numeric suffix if another playbook claimed its name in the meantime."),
		Args:  cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			return runPlaybookRestore(api, args[0])
		}),
	}
}

func playbookSaveDraftCommand(accountID string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "save-draft <playbook-reference>",
		Short: "Save a definition as the playbook's unpublished draft",
		Long: cli.Markdown(`Stores a definition as the playbook's draft, read from ` + "`--definition-file`" + ` or stdin.

Saving is not publishing. The active version, what running tasks follow, and what a new
task pins to are all unchanged; ` + "`handbook read`" + ` still returns the published
definition. There is one draft per playbook and saving again overwrites it.

Drafts are stored verbatim and are not validated until publication, so a draft that the
server will reject is accepted here.`),
		Args: cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			definition, err := definitionFromFlags(cmd)
			if err != nil {
				return err
			}
			return runPlaybookSaveDraft(api, args[0], definition)
		}),
	}
	addDefinitionFlags(cmd)
	return cmd
}

func playbookDiffDraftCommand(accountID string) *cobra.Command {
	return &cobra.Command{
		Use:   "diff-draft <playbook-reference>",
		Short: "Compare the saved draft with the active published definition",
		Long:  cli.Markdown("Reads the draft and the active version and compares them here, field by field. Nothing is written: the draft stays unpublished and the active version stays active. A playbook with no published version yet reports every field of the draft as added."),
		Args:  cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			return runPlaybookDiffDraft(api, args[0])
		}),
	}
}

func playbookDiscardDraftCommand(accountID string) *cobra.Command {
	return &cobra.Command{
		Use:   "discard-draft <playbook-reference>",
		Short: "Discard the saved draft, leaving the published definition alone",
		Long:  cli.Markdown("Clears the draft through its own API operation. The active version is untouched, so the playbook keeps running exactly as it did; what is lost is the unpublished working copy."),
		Args:  cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			return runPlaybookDiscardDraft(api, args[0])
		}),
	}
}

func playbookPublishCommand(accountID string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish <playbook-reference>",
		Short: "Publish the saved draft as a new version",
		Long: cli.Markdown(`Publishes the playbook's saved draft: the draft is read back, sent to the version
endpoint, validated server-side, and stored as the next sequential version. This is the
only command here that changes what the playbook runs.

A playbook with no saved draft fails without publishing anything. Validation failures are
the server's and are reported as they come back, with no version claimed.

` + "`--activate=false`" + ` publishes the version without making it active; the result
reports the new version and the still-active version separately. Publishing never enables
a disabled playbook: a disabled playbook is still disabled afterwards, and
` + "`handbook update-playbook --enable`" + ` is what turns routing back on.

Tasks already running stay pinned to the version they were created against. Page content
is not versioned this way: a page's revisions and the playbook's linked-page set reach
later runs as soon as they are saved, with no publish involved.

To submit a definition directly without saving it as a draft first, the low-level
` + "`wallfacer versions create <playbook-id>`" + ` still takes one.`),
		Args: cobra.ExactArgs(1),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			activate, _ := cmd.Flags().GetBool("activate")
			notes, _ := cmd.Flags().GetString("notes")
			return runPlaybookPublish(api, args[0], notes, activate)
		}),
	}
	cmd.Flags().Bool("activate", true, "Make the published version the active one")
	cmd.Flags().String("notes", "", "Notes recorded with the version")
	return cmd
}

func playbookDiffCommand(accountID string) *cobra.Command {
	return &cobra.Command{
		Use:   "diff <playbook-reference> <a> <b>",
		Short: "Compare two published versions of a playbook",
		Long:  cli.Markdown("Each side is a version number or a version UUID. The API returns both versions' definitions side by side, not a change list, and the comparison is a read: no version is published, activated, or altered to produce it. To compare an unpublished draft against the active version and get a change list, use `handbook diff-draft`."),
		Args:  cobra.ExactArgs(3),
		Run: handbookRun(accountID, func(api *handbookAPI, cmd *cobra.Command, args []string) error {
			return runPlaybookDiff(api, args[0], args[1], args[2])
		}),
	}
}

func addDefinitionFlags(cmd *cobra.Command) {
	cmd.Flags().String("definition-file", "", "File holding the definition as JSON or YAML; omit to read stdin")
}

func definitionFromFlags(cmd *cobra.Command) (map[string]interface{}, error) {
	path, _ := cmd.Flags().GetString("definition-file")
	return loadDefinition(path)
}

// loadDefinition reads a definition document from a file or from stdin. Both
// JSON and YAML are accepted, and the wrappers around a definition — the API's
// `{"definition": ...}` request envelope and what `handbook version` and
// `handbook draft` print — are unwrapped, so a definition can be round-tripped
// out of a read and back into a draft.
func loadDefinition(path string) (map[string]interface{}, error) {
	var raw []byte
	var err error

	if path == "" || path == "-" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, errors.Wrap(err, "reading the definition")
	}
	if strings.TrimSpace(string(raw)) == "" {
		return nil, errors.New("no definition supplied: pass --definition-file <path>, or pipe the definition on stdin")
	}

	document, err := decodeDefinition(raw)
	if err != nil {
		return nil, err
	}
	return unwrapDefinition(document), nil
}

func decodeDefinition(raw []byte) (map[string]interface{}, error) {
	var decoded interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		var asYAML interface{}
		if yamlErr := yaml.Unmarshal(raw, &asYAML); yamlErr != nil {
			return nil, errors.Errorf("the definition is neither JSON nor YAML: %v", yamlErr)
		}
		decoded = normalizeYAML(asYAML)
	}

	document, ok := decoded.(map[string]interface{})
	if !ok {
		return nil, errors.New("the definition must be a JSON or YAML object")
	}
	return document, nil
}

// cliEnvelopeKeys are the keys every handbook response is printed with. They
// accompany the payload rather than being part of it, so a document carrying
// `data` and nothing but these is a printed CLI response.
var cliEnvelopeKeys = map[string]bool{
	"reference":  true,
	"follow_up":  true,
	"pagination": true,
	"versions":   true,
}

// unwrapDefinition accepts the API's request envelope and the shapes the CLI
// prints as well as the bare definition, so a definition read with `handbook
// version` or `handbook draft` goes straight back into a draft. A definition
// never carries a top-level `definition` object of its own, so wherever one
// appears it is the wrapper that is being peeled.
func unwrapDefinition(document map[string]interface{}) map[string]interface{} {
	for i := 0; i < 4; i++ {
		inner, wrapped := unwrapDefinitionOnce(document)
		if !wrapped {
			return document
		}
		document = inner
	}
	return document
}

func unwrapDefinitionOnce(document map[string]interface{}) (map[string]interface{}, bool) {
	// `{"definition": ...}` — the API's request envelope, and the version
	// record `handbook version` prints under `data`.
	if inner, ok := document["definition"].(map[string]interface{}); ok {
		return inner, true
	}

	// `{"data": ..., "reference": ..., "follow_up": ...}` — a printed CLI
	// response, whose payload is what was meant.
	if inner, ok := document["data"].(map[string]interface{}); ok {
		for key := range document {
			if key != "data" && !cliEnvelopeKeys[key] {
				return nil, false
			}
		}
		return inner, true
	}

	// `{"present": true, "draft": ...}` — what `handbook draft` prints.
	if inner, ok := document["draft"].(map[string]interface{}); ok {
		if _, ok := inner["definition"]; ok {
			return inner, true
		}
	}

	return nil, false
}

// normalizeYAML converts what yaml.v2 decodes into the map shapes the rest of
// the CLI (and encoding/json) works with.
func normalizeYAML(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[interface{}]interface{}:
		out := map[string]interface{}{}
		for key, item := range typed {
			out[fmt.Sprintf("%v", key)] = normalizeYAML(item)
		}
		return out
	case map[string]interface{}:
		out := map[string]interface{}{}
		for key, item := range typed {
			out[key] = normalizeYAML(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, item := range typed {
			out[i] = normalizeYAML(item)
		}
		return out
	default:
		return value
	}
}

// playbookUpdate is the set of metadata fields an update was actually asked to
// change. Only the fields the caller named are sent, so an update never
// restates — or silently resets — anything else.
type playbookUpdate struct {
	body           map[string]interface{}
	linkedPageRefs []string
}

func playbookMetadataUpdate(cmd *cobra.Command) (*playbookUpdate, error) {
	update := &playbookUpdate{body: map[string]interface{}{}}

	if cmd.Flags().Changed("name") {
		name, _ := cmd.Flags().GetString("name")
		update.body["name"] = name
	}

	clearDescription, _ := cmd.Flags().GetBool("clear-description")
	if cmd.Flags().Changed("description") && clearDescription {
		return nil, errors.New("--description and --clear-description cannot both be given")
	}
	if cmd.Flags().Changed("description") {
		description, _ := cmd.Flags().GetString("description")
		update.body["description"] = description
	}
	if clearDescription {
		update.body["description"] = nil
	}

	disable, _ := cmd.Flags().GetBool("disable")
	enable, _ := cmd.Flags().GetBool("enable")
	if disable && enable {
		return nil, errors.New("--disable and --enable cannot both be given")
	}
	if disable {
		update.body["disabled"] = true
	}
	if enable {
		update.body["disabled"] = false
	}

	clearLinked, _ := cmd.Flags().GetBool("clear-linked-pages")
	linked, _ := cmd.Flags().GetStringArray("link-page")
	if len(linked) > 0 && clearLinked {
		return nil, errors.New("--link-page and --clear-linked-pages cannot both be given")
	}
	if clearLinked {
		update.body["linked_page_ids"] = []string{}
	}
	if len(linked) > 0 {
		update.linkedPageRefs = linked
	}

	if len(update.body) == 0 && len(update.linkedPageRefs) == 0 {
		return nil, errors.New("nothing to update: name a field to change")
	}
	return update, nil
}

func runPlaybookCreate(api *handbookAPI, name, description, parent string, linkedRefs []string, definition map[string]interface{}, alsoDraft bool) error {
	body := map[string]interface{}{
		"name":       name,
		"definition": definition,
	}
	if description != "" {
		body["description"] = description
	}

	// Page references are resolved before the create call, so a reference to
	// the wrong type or to another account fails without writing anything.
	if parent != "" {
		parentRef, err := api.resolveHandbookRef(parent, kindPage)
		if err != nil {
			return err
		}
		body["parent_page_id"] = parentRef.ID
	}
	linkedIDs, err := api.resolvePageIDs(linkedRefs)
	if err != nil {
		return err
	}
	if linkedIDs != nil {
		body["linked_page_ids"] = linkedIDs
	}

	created, err := api.createPipeline(body)
	if err != nil {
		return err
	}

	ref := refFromPipelineRecord(created, api.accountID, "created")
	data := map[string]interface{}{
		"playbook":       created,
		"active_version": created["active_version"],
	}

	if alsoDraft {
		if _, err := api.savePipelineDraft(ref.ID, definition); err != nil {
			return errors.Wrapf(err, "the playbook was created as %s, but saving its draft", ref.ID)
		}
		data["draft"] = map[string]interface{}{"present": true, "definition": definition}
	}

	return emitHandbook(map[string]interface{}{
		"data":      data,
		"reference": ref,
		"follow_up": map[string]interface{}{
			"read":       fmt.Sprintf("wallfacer handbook read %s", ref.ID),
			"versions":   fmt.Sprintf("wallfacer handbook versions %s", ref.ID),
			"save_draft": fmt.Sprintf("wallfacer handbook save-draft %s", ref.ID),
		},
	})
}

func runPlaybookUpdate(api *handbookAPI, reference string, update *playbookUpdate) error {
	ref, err := api.resolveHandbookRef(reference, kindPlaybook)
	if err != nil {
		return err
	}

	// Enabling an archived playbook is the one metadata change the server
	// refuses outright, and its 422 names the `archived: false` request field
	// rather than the command that restores one. Say it in the CLI's own
	// vocabulary, before anything is sent.
	if enabled, ok := update.body["disabled"].(bool); ok && !enabled && ref.State == "archived" {
		return errors.Errorf("playbook %s is archived, so enabling it is refused; restore it first with: wallfacer handbook restore-playbook %s", ref.ID, ref.ID)
	}

	if len(update.linkedPageRefs) > 0 {
		linkedIDs, err := api.resolvePageIDs(update.linkedPageRefs)
		if err != nil {
			return err
		}
		update.body["linked_page_ids"] = linkedIDs
	}

	record, err := api.updatePipeline(ref.ID, update.body)
	if err != nil {
		return handbookReadError(err, ref)
	}

	updated := refFromPipelineRecord(record, api.accountID, ref.ResolvedFrom)
	return emitHandbook(map[string]interface{}{
		"data": map[string]interface{}{
			"playbook": record,
			"changed":  sortedKeys(update.body),
			// Metadata is not the definition: the active version is
			// reported so it is visible that it did not move.
			"active_version": record["active_version"],
		},
		"reference": updated,
		"follow_up": api.followUp(updated),
	})
}

func runPlaybookArchive(api *handbookAPI, reference string) error {
	ref, err := api.resolveHandbookRef(reference, kindPlaybook)
	if err != nil {
		return err
	}

	// The server short-circuits an already-archived playbook: it answers 204
	// without writing, so the state is checked here rather than reporting an
	// archive that did not happen.
	if ref.State == "archived" {
		return errors.Errorf("playbook %s is already archived", ref.ID)
	}

	if _, err := api.archivePipeline(ref.ID); err != nil {
		return handbookReadError(err, ref)
	}

	ref.State = "archived"
	return emitHandbook(map[string]interface{}{
		"data": map[string]interface{}{
			"id":       ref.ID,
			"archived": true,
		},
		"reference": ref,
		"follow_up": map[string]interface{}{
			"restore": fmt.Sprintf("wallfacer handbook restore-playbook %s", ref.ID),
			"list":    "wallfacer handbook list --type playbook --include-archived",
		},
	})
}

func runPlaybookRestore(api *handbookAPI, reference string) error {
	ref, err := api.resolveHandbookRef(reference, kindPlaybook)
	if err != nil {
		return err
	}
	// The server's PATCH is idempotent: on a playbook that was never archived
	// it writes nothing and still answers 200, so the state is checked here
	// rather than reporting a restore that did not happen.
	if ref.State != "archived" {
		return errors.Errorf("playbook %s is not archived, so there is nothing to restore", ref.ID)
	}

	record, err := api.updatePipeline(ref.ID, map[string]interface{}{"archived": false})
	if err != nil {
		return handbookReadError(err, ref)
	}

	restored := refFromPipelineRecord(record, api.accountID, ref.ResolvedFrom)
	return emitHandbook(map[string]interface{}{
		"data": map[string]interface{}{
			"playbook": record,
			// A restore always comes back disabled, and the name may have
			// been suffixed; both are reported rather than assumed.
			"disabled": record["disabled_at"] != nil,
			"name":     stringField(record, "name"),
		},
		"reference": restored,
		"follow_up": map[string]interface{}{
			"enable": fmt.Sprintf("wallfacer handbook update-playbook %s --enable", restored.ID),
			"read":   fmt.Sprintf("wallfacer handbook read %s", restored.ID),
		},
	})
}

func runPlaybookSaveDraft(api *handbookAPI, reference string, definition map[string]interface{}) error {
	ref, err := api.resolveHandbookRef(reference, kindPlaybook)
	if err != nil {
		return err
	}

	saved, err := api.savePipelineDraft(ref.ID, definition)
	if err != nil {
		return handbookReadError(err, ref)
	}

	data := map[string]interface{}{
		"saved":     true,
		"published": false,
		"draft":     draftRecordOf(saved, definition),
	}
	if ref.ActiveVersion != nil {
		data["active_version"] = ref.ActiveVersion
	} else {
		data["active_version"] = nil
	}

	return emitHandbook(map[string]interface{}{
		"data":      data,
		"reference": ref,
		"follow_up": map[string]interface{}{
			"diff":    fmt.Sprintf("wallfacer handbook diff-draft %s", ref.ID),
			"publish": fmt.Sprintf("wallfacer handbook publish %s", ref.ID),
			"discard": fmt.Sprintf("wallfacer handbook discard-draft %s", ref.ID),
		},
	})
}

func runPlaybookDiscardDraft(api *handbookAPI, reference string) error {
	ref, err := api.resolveHandbookRef(reference, kindPlaybook)
	if err != nil {
		return err
	}

	if _, err := api.discardPipelineDraft(ref.ID); err != nil {
		return handbookReadError(err, ref)
	}

	// The published definition is untouched by a discard; reading the
	// playbook back is what says so rather than this command asserting it.
	record, err := api.getPipeline(ref.ID)
	if err != nil {
		return err
	}

	return emitHandbook(map[string]interface{}{
		"data": map[string]interface{}{
			"discarded":      true,
			"draft":          map[string]interface{}{"present": record["draft"] != nil},
			"active_version": record["active_version"],
		},
		"reference": refFromPipelineRecord(record, api.accountID, ref.ResolvedFrom),
		"follow_up": map[string]interface{}{
			"read":    fmt.Sprintf("wallfacer handbook read %s", ref.ID),
			"version": fmt.Sprintf("wallfacer handbook version %s", ref.ID),
		},
	})
}

func runPlaybookDiffDraft(api *handbookAPI, reference string) error {
	ref, err := api.resolveHandbookRef(reference, kindPlaybook)
	if err != nil {
		return err
	}

	record, err := api.getPipeline(ref.ID)
	if err != nil {
		return handbookReadError(err, ref)
	}

	draft, definition, err := draftDefinitionOf(record, ref.ID)
	if err != nil {
		return err
	}

	var active map[string]interface{}
	var activeDefinition interface{}
	if summary, ok := record["active_version"].(map[string]interface{}); ok {
		version := versionArgument(summary["version"])
		if version == "" {
			version = versionArgument(summary["id"])
		}
		if version != "" {
			active, err = api.getPipelineVersion(ref.ID, version)
			if err != nil && err != errHandbookNotFound {
				return err
			}
		}
	}
	if active != nil {
		activeDefinition = active["definition"]
	}

	changes := definitionChanges(activeDefinition, definition)
	data := map[string]interface{}{
		"identical": len(changes) == 0,
		"changes":   changes,
		"draft": map[string]interface{}{
			"updated_at": draft["updated_at"],
			"updated_by": draft["updated_by"],
		},
		"published": false,
	}
	if active != nil {
		data["active_version"] = map[string]interface{}{"id": active["id"], "version": active["version"]}
	} else {
		data["active_version"] = nil
		data["note"] = "the playbook has no published version; every field of the draft is new"
	}

	return emitHandbook(map[string]interface{}{
		"data":      data,
		"reference": ref,
		"follow_up": map[string]interface{}{
			"draft":   fmt.Sprintf("wallfacer handbook draft %s", ref.ID),
			"publish": fmt.Sprintf("wallfacer handbook publish %s", ref.ID),
		},
	})
}

func runPlaybookPublish(api *handbookAPI, reference, notes string, activate bool) error {
	ref, err := api.resolveHandbookRef(reference, kindPlaybook)
	if err != nil {
		return err
	}

	record, err := api.getPipeline(ref.ID)
	if err != nil {
		return handbookReadError(err, ref)
	}

	// Publication operates on the saved draft. A playbook with no draft, or
	// with a draft the API stored verbatim and that holds no definition, is
	// reported as such before anything is sent to the version endpoint.
	_, definition, err := draftDefinitionOf(record, ref.ID)
	if err != nil {
		return err
	}

	body := map[string]interface{}{"definition": definition}
	if notes != "" {
		body["notes"] = notes
	}
	if !activate {
		body["activate"] = false
	}

	published, err := api.publishPipelineVersion(ref.ID, body)
	if err != nil {
		// The server validates at publish time. Nothing was versioned, so
		// nothing is reported as one.
		return errors.Wrapf(err, "publishing the draft of playbook %s failed; no version was created", ref.ID)
	}

	// The version endpoint answers out of the row it just inserted, and
	// created_at is a database default that insert does not read back, so the
	// timestamp can arrive null on a record that has one. Read the version to
	// report the real timestamp rather than a null the next command
	// contradicts.
	if published["created_at"] == nil {
		if version := versionArgument(published["version"]); version != "" {
			if stored, err := api.getPipelineVersion(ref.ID, version); err == nil && stored["created_at"] != nil {
				published["created_at"] = stored["created_at"]
			}
		}
	}

	// Read the playbook back: which version is active, whether the draft was
	// cleared, and whether the playbook is still disabled are the server's
	// answers, not this command's.
	after, err := api.getPipeline(ref.ID)
	if err != nil {
		return err
	}

	data := map[string]interface{}{
		"published_version": published,
		"activated":         activate,
		"active_version":    after["active_version"],
		"disabled":          after["disabled_at"] != nil,
		"draft":             map[string]interface{}{"present": after["draft"] != nil},
	}
	if !activate {
		data["note"] = "published without activating: the active version is unchanged and new tasks still pin to it"
	}
	if after["disabled_at"] != nil {
		data["disabled_note"] = "the playbook is disabled and publishing did not enable it; use handbook update-playbook --enable"
	}

	updated := refFromPipelineRecord(after, api.accountID, ref.ResolvedFrom)
	return emitHandbook(map[string]interface{}{
		"data":      data,
		"reference": updated,
		"follow_up": map[string]interface{}{
			"version":  fmt.Sprintf("wallfacer handbook version %s %s", ref.ID, versionArgument(published["version"])),
			"versions": fmt.Sprintf("wallfacer handbook versions %s", ref.ID),
			"read":     fmt.Sprintf("wallfacer handbook read %s", ref.ID),
		},
	})
}

func runPlaybookDiff(api *handbookAPI, reference, a, b string) error {
	ref, err := api.resolveHandbookRef(reference, kindPlaybook)
	if err != nil {
		return err
	}

	diff, err := api.diffPipelineVersions(ref.ID, a, b)
	if err != nil {
		if err == errHandbookNotFound {
			return errors.Errorf("playbook %s does not have both versions %s and %s", ref.ID, a, b)
		}
		return err
	}

	return emitHandbook(map[string]interface{}{
		// The diff endpoint answers under `data` already; emitting the
		// response as-is would nest it a second time and break the query
		// projection every other handbook command shares.
		"data":      responseData(diff),
		"reference": ref,
		"versions":  map[string]interface{}{"a": a, "b": b},
		"follow_up": map[string]interface{}{
			"a":        fmt.Sprintf("wallfacer handbook version %s %s", ref.ID, a),
			"b":        fmt.Sprintf("wallfacer handbook version %s %s", ref.ID, b),
			"versions": fmt.Sprintf("wallfacer handbook versions %s", ref.ID),
		},
	})
}

// draftDefinitionOf pulls the stored draft's definition out of a playbook
// record. Drafts are stored verbatim, so a saved draft that holds no definition
// object is a real case and is reported as one rather than published as an
// empty definition.
func draftDefinitionOf(record map[string]interface{}, pipelineID string) (map[string]interface{}, map[string]interface{}, error) {
	draft, ok := record["draft"].(map[string]interface{})
	if !ok {
		return nil, nil, errors.Errorf("playbook %s has no saved draft; save one with: wallfacer handbook save-draft %s", pipelineID, pipelineID)
	}
	definition, ok := draft["definition"].(map[string]interface{})
	if !ok {
		return nil, nil, errors.Errorf("the saved draft of playbook %s holds no definition object; drafts are stored verbatim, so save a valid one before publishing", pipelineID)
	}
	return draft, definition, nil
}

// draftRecordOf prefers whatever the save returned and falls back to what was
// sent, since the draft endpoint's response body is not part of its contract.
// The save answers with the whole playbook record under `data`, so the draft
// itself is lifted out of it rather than reported as the playbook.
func draftRecordOf(saved map[string]interface{}, definition map[string]interface{}) map[string]interface{} {
	if saved != nil {
		if data, ok := saved["data"].(map[string]interface{}); ok {
			return draftWithin(data, definition)
		}
		if len(saved) > 0 {
			return draftWithin(saved, definition)
		}
	}
	return map[string]interface{}{"definition": definition}
}

func draftWithin(record map[string]interface{}, definition map[string]interface{}) map[string]interface{} {
	if draft, ok := record["draft"].(map[string]interface{}); ok {
		return draft
	}
	if _, ok := record["definition"]; ok {
		return record
	}
	return map[string]interface{}{"definition": definition}
}

// definitionChanges compares two definitions field by field and returns one
// entry per differing path. It is a local comparison: no version is published
// or altered to produce it.
func definitionChanges(active, draft interface{}) []map[string]interface{} {
	changes := []map[string]interface{}{}
	collectChanges("", active, draft, &changes)
	sort.Slice(changes, func(i, j int) bool {
		return changes[i]["path"].(string) < changes[j]["path"].(string)
	})
	return changes
}

func collectChanges(path string, active, draft interface{}, changes *[]map[string]interface{}) {
	activeMap, activeIsMap := active.(map[string]interface{})
	draftMap, draftIsMap := draft.(map[string]interface{})
	if activeIsMap && draftIsMap {
		for _, key := range unionKeys(activeMap, draftMap) {
			collectChanges(joinPath(path, key), activeMap[key], draftMap[key], changes)
		}
		return
	}

	activeList, activeIsList := active.([]interface{})
	draftList, draftIsList := draft.([]interface{})
	if activeIsList && draftIsList {
		longest := len(activeList)
		if len(draftList) > longest {
			longest = len(draftList)
		}
		for i := 0; i < longest; i++ {
			collectChanges(joinPath(path, "["+strconv.Itoa(i)+"]"), itemAt(activeList, i), itemAt(draftList, i), changes)
		}
		return
	}

	if sameScalar(active, draft) {
		return
	}

	change := map[string]interface{}{"path": path, "active": active, "draft": draft}
	switch {
	case active == nil:
		change["change"] = "added"
	case draft == nil:
		change["change"] = "removed"
	default:
		change["change"] = "changed"
	}
	*changes = append(*changes, change)
}

func sameScalar(a, b interface{}) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return string(left) == string(right)
}

func unionKeys(a, b map[string]interface{}) []string {
	seen := map[string]bool{}
	keys := []string{}
	for _, source := range []map[string]interface{}{a, b} {
		for key := range source {
			if !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

func itemAt(list []interface{}, index int) interface{} {
	if index < len(list) {
		return list[index]
	}
	return nil
}

func joinPath(prefix, segment string) string {
	switch {
	case prefix == "":
		return segment
	case strings.HasPrefix(segment, "["):
		return prefix + segment
	default:
		return prefix + "." + segment
	}
}

func sortedKeys(body map[string]interface{}) []string {
	keys := []string{}
	for key := range body {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
