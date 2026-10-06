package main

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pmezard/go-difflib/difflib"
	"gopkg.in/yaml.v2"
)

// Text views of the handbook commands. Each renderer reads the same payload
// -o json prints. Pages come out as markdown files with YAML frontmatter;
// playbook definitions come out as YAML that saves straight back into a draft;
// lists come out one entry per line with the ID the next command takes. A view
// of one record ends with a Next: block of the commands for that record, each
// labelled with the entry it reaches.

// annotator labels a follow-up command with the title of the entry it reads,
// so a list of child commands says which child is which. The entry the result
// is about is left unlabelled: its title is already at the top.
func (a *handbookAPI) annotator(self string) func(string) string {
	return func(cmd string) string {
		if a.index == nil {
			return ""
		}
		fields := strings.Fields(cmd)
		for i := len(fields) - 1; i >= 0; i-- {
			if !uuidPattern.MatchString(fields[i]) {
				continue
			}
			if fields[i] == self {
				return ""
			}
			if ref, ok := a.index.byID[fields[i]]; ok {
				return fmt.Sprintf("%s (%s)", ref.Title, ref.Type)
			}
			return ""
		}
		return ""
	}
}

// entryKind is the short type column of an entry line: "page", "page, empty",
// "playbook v4", "playbook v4, disabled, draft saved".
func entryKind(entry map[string]interface{}) string {
	kind := scalar(entry["type"])
	var notes []string
	switch kind {
	case kindPage:
		if hasBody, ok := entry["has_body"].(bool); ok && !hasBody {
			notes = append(notes, "empty")
		}
	case kindPlaybook:
		if version := scalar(asMap(entry["active_version"])["version"]); version != "" {
			kind += " v" + version
		} else {
			notes = append(notes, "unpublished")
		}
		if scalar(entry["disabled_at"]) != "" {
			notes = append(notes, "disabled")
		}
		if hasDraft, ok := entry["has_draft"].(bool); ok && hasDraft {
			notes = append(notes, "draft saved")
		}
	}
	if state := scalar(entry["state"]); state != "" && state != "active" && !slices.Contains(notes, state) {
		notes = append(notes, state)
	}
	if len(notes) > 0 {
		kind += ", " + strings.Join(notes, ", ")
	}
	return kind
}

// entryLine is one handbook entry on one line: label, type, ID, description.
func entryLine(indent, label string, entry map[string]interface{}) string {
	line := fmt.Sprintf("%s- %s · %s · %s", indent, label, entryKind(entry), scalar(entry["id"]))
	if description := oneLine(scalar(entry["description"])); description != "" {
		line += " — " + description
	}
	return line
}

// stamp shortens an API timestamp for reading: 2026-09-23T23:09:08.000000Z
// becomes 2026-09-23T23:09:08Z. Anything that does not parse prints as-is.
func stamp(value interface{}) string {
	raw := scalar(value)
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return raw
	}
	return parsed.UTC().Format(time.RFC3339)
}

// dropSpentNextPage removes the next-page command once the listing is on its
// last page, where it would point at nothing.
func dropSpentNextPage(followUp interface{}, pagination map[string]interface{}) map[string]interface{} {
	out := withoutKeys(followUp)
	meta := asMap(pagination["meta"])
	current := atoi(scalar(meta["current_page"]))
	if current >= atoi(scalar(meta["last_page"])) {
		delete(out, "next_page")
	} else if cmd, ok := out["next_page"].(string); ok {
		out["next_page"] = strings.Replace(cmd, "<n>", fmt.Sprint(current+1), 1)
	}
	return out
}

// markdownBody drops the blank lines around stored markdown and keeps every
// other character, so a body that opens with an indented code block still
// renders as one.
func markdownBody(text string) string {
	text = strings.TrimRight(text, " \t\r\n")
	for {
		line, rest, found := strings.Cut(text, "\n")
		if !found || strings.TrimSpace(line) != "" {
			return text
		}
		text = rest
	}
}

// shellQuote renders an argument so a printed command means the same thing
// when pasted into a POSIX shell: plain words stay bare, anything else is
// single-quoted.
func shellQuote(arg string) string {
	if arg != "" && strings.Trim(arg, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-./:@%+=,") == "" {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// tree

func renderHandbookTree(under string) textRenderer {
	return func(t *textOut, p map[string]interface{}) {
		data := asMap(p["data"])
		nodes := asList(data["tree"])

		pages, playbooks := countTree(nodes)
		scope := "Handbook"
		if under != "" {
			scope = fmt.Sprintf("Handbook under %s", under)
		}
		summary := fmt.Sprintf("%s: %s, %s", scope, plural(pages, "page", "pages"), plural(playbooks, "playbook", "playbooks"))
		if hidden := countHidden(nodes); hidden > 0 {
			summary += fmt.Sprintf(" shown, %s deeper (raise --depth to see them)", plural(hidden, "more entry", "more entries"))
		}
		t.line("%s.", summary)
		t.gap()
		if len(nodes) == 0 {
			t.line("(nothing filed here)")
			return
		}
		writeTreeNodes(t, nodes, 0)
		t.gap()
		t.line("Read one: wallfacer handbook read <id>")
	}
}

func writeTreeNodes(t *textOut, nodes []interface{}, level int) {
	indent := strings.Repeat("  ", level)
	for _, item := range nodes {
		node := asMap(item)
		if node == nil {
			continue
		}
		line := entryLine(indent, scalar(node["title"]), node)
		if hidden, ok := node["children_hidden"].(float64); ok && hidden > 0 {
			line += fmt.Sprintf(" [+%s below]", plural(int(hidden), "entry", "entries"))
		}
		t.text(line)
		writeTreeNodes(t, asList(node["children"]), level+1)
	}
}

func countHidden(nodes []interface{}) int {
	hidden := 0
	for _, item := range nodes {
		node := asMap(item)
		if n, ok := node["children_hidden"].(float64); ok {
			hidden += int(n)
		}
		hidden += countHidden(asList(node["children"]))
	}
	return hidden
}

func countTree(nodes []interface{}) (pages, playbooks int) {
	for _, item := range nodes {
		node := asMap(item)
		switch scalar(node["type"]) {
		case kindPage:
			pages++
		case kindPlaybook:
			playbooks++
		}
		p, b := countTree(asList(node["children"]))
		pages += p
		playbooks += b
	}
	return pages, playbooks
}

// list and search

func renderHandbookList(listCommand func(page int) string) textRenderer {
	return func(t *textOut, p map[string]interface{}) {
		entries := asList(p["data"])
		if len(entries) == 0 {
			t.line("No handbook entries on this page of results.")
		}
		for _, item := range entries {
			entry := asMap(item)
			label := scalar(entry["path"])
			if label == "" {
				label = scalar(entry["title"])
			}
			t.text(entryLine("", label, entry))
		}

		pagination := asMap(p["pagination"])
		var summary []string
		nextPage := 0
		for _, kind := range []string{"pages", "playbooks"} {
			meta := asMap(asMap(pagination[kind])["meta"])
			if meta == nil {
				continue
			}
			current, last, total := scalar(meta["current_page"]), scalar(meta["last_page"]), scalar(meta["total"])
			summary = append(summary, fmt.Sprintf("%s: page %s of %s (%s total)", kind, current, last, total))
			if c, l := atoi(current), atoi(last); c < l && c+1 > nextPage {
				nextPage = c + 1
			}
		}
		if len(summary) > 0 {
			t.gap()
			t.text(strings.Join(summary, " · "))
			if nextPage > 0 {
				t.line("More: %s", listCommand(nextPage))
			}
		}
		if len(entries) > 0 {
			t.gap()
			t.line("Read one: wallfacer handbook read <id>")
		}
	}
}

func renderHandbookSearch(maxPages int) textRenderer {
	return func(t *textOut, p map[string]interface{}) {
		matches := asList(p["data"])
		query := scalar(p["query"])
		incomplete := false
		for _, sweep := range asMap(p["pagination"]) {
			if complete, ok := asMap(sweep)["complete"].(bool); ok && !complete {
				incomplete = true
			}
		}
		stopped := func() {
			t.line("Stopped after %d result pages per type. Search further: wallfacer handbook search %s --max-pages %d", maxPages, shellQuote(query), maxPages*2)
		}

		if len(matches) == 0 {
			t.line("No pages or playbooks match %q in what was searched.", query)
			if incomplete {
				stopped()
			} else {
				t.line("Try a shorter or different word, or browse with `wallfacer handbook tree`.")
			}
			return
		}
		header := plural(len(matches), "match", "matches")
		if total := atoi(scalar(p["total"])); total > len(matches) {
			header = fmt.Sprintf("Best %d of %d matches", len(matches), total)
		}
		t.line("%s for %q, title matches first:", header, query)
		t.gap()
		for _, item := range matches {
			entry := asMap(item)
			label := scalar(entry["path"])
			if label == "" {
				label = scalar(entry["title"])
			}
			t.text(entryLine("", label, entry))
			var matched []string
			for _, f := range asList(entry["matched_in"]) {
				matched = append(matched, scalar(f))
			}
			detail := "  matched in " + strings.Join(matched, ", ")
			if snippet := scalar(entry["snippet"]); snippet != "" {
				detail += ": “" + snippet + "”"
			}
			t.text(detail)
		}

		t.gap()
		if truncated, _ := p["truncated"].(bool); truncated {
			t.line("See more: wallfacer handbook search %s --limit %s", shellQuote(query), scalar(p["total"]))
		}
		if incomplete {
			stopped()
		}
		t.line("Read one: wallfacer handbook read <id>")
	}
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// snippetAround returns the text around the first case-insensitive match of
// needle, on one line, so a body match shows why it matched.
func snippetAround(text, needle string, radius int) string {
	flat := oneLine(text)
	lower := strings.ToLower(flat)
	if len(lower) != len(flat) {
		// A few characters change byte length when lowercased; quote the
		// lowercased text rather than index into the original with a
		// position that no longer lines up.
		flat = lower
	}
	at := strings.Index(lower, strings.ToLower(needle))
	if needle == "" || at < 0 {
		return ""
	}
	start, end := at-radius, at+len(needle)+radius
	prefix, suffix := "…", "…"
	if start <= 0 {
		start, prefix = 0, ""
	}
	if end >= len(flat) {
		end, suffix = len(flat), ""
	}
	// Keep the cut on rune boundaries.
	for start > 0 && !utf8.RuneStart(flat[start]) {
		start--
	}
	for end < len(flat) && !utf8.RuneStart(flat[end]) {
		end++
	}
	return prefix + flat[start:end] + suffix
}

// reads

func referenceFields(ref map[string]interface{}) []field {
	return []field{
		{"type", scalar(ref["type"])},
		{"id", scalar(ref["id"])},
		{"title", scalar(ref["title"])},
		{"path", scalar(ref["path"])},
		{"state", scalar(ref["state"])},
		{"description", oneLine(scalar(ref["description"]))},
	}
}

func withoutField(fields []field, key string) []field {
	var out []field
	for _, f := range fields {
		if f.key != key {
			out = append(out, f)
		}
	}
	return out
}

func (a *handbookAPI) parentField(ref map[string]interface{}) field {
	parentID := scalar(ref["parent_page_id"])
	if parentID == "" {
		return field{"parent", "(top level)"}
	}
	if a.index != nil {
		if parent, ok := a.index.byID[parentID]; ok {
			return field{"parent", fmt.Sprintf("%s · %s", parent.Title, parentID)}
		}
	}
	return field{"parent", parentID}
}

func renderHandbookRead(api *handbookAPI) textRenderer {
	return func(t *textOut, p map[string]interface{}) {
		ref := asMap(p["reference"])
		if scalar(ref["type"]) == kindPlaybook {
			renderPlaybookRead(api, t, p)
			return
		}

		record := asMap(p["data"])
		fields := append(referenceFields(ref), api.parentField(ref), field{"updated_at", stamp(record["updated_at"])})
		t.frontmatter(fields...)
		t.gap()
		t.line("# %s", scalar(record["title"]))
		t.gap()
		if body := markdownBody(scalar(record["body"])); body != "" {
			t.block(body)
		} else {
			t.line("_This page has no body yet._")
		}
		t.gap()
		t.line("---")
		id := scalar(ref["id"])
		followUp := withoutKeys(p["follow_up"], "read")
		followUp["body"] = fmt.Sprintf("wallfacer handbook read %s --body > page.md", id)
		followUp["edit"] = fmt.Sprintf("wallfacer handbook update %s --body-file page.md", id)
		t.next(followUp, api.annotator(id))
	}
}

// withoutKeys copies a follow_up map without some of its commands; a read
// does not need to offer reading the entry it just printed.
func withoutKeys(followUp interface{}, keys ...string) map[string]interface{} {
	out := map[string]interface{}{}
	for key, value := range asMap(followUp) {
		out[key] = value
	}
	for _, key := range keys {
		delete(out, key)
	}
	return out
}

func renderPlaybookRead(api *handbookAPI, t *textOut, p map[string]interface{}) {
	ref := asMap(p["reference"])
	record := asMap(p["data"])
	active := asMap(record["active_version"])
	definition := asMap(active["definition"])
	draft := asMap(record["draft"])

	hasDraft, _ := draft["present"].(bool)
	// Versions can exist with none active: one published with --activate=false.
	versionCount := atoi(scalar(record["version_count"]))

	activeField := "none published yet"
	if version := scalar(active["version"]); version != "" {
		activeField = "v" + version
		if count := scalar(record["version_count"]); count != "" {
			activeField += " of " + count
		}
	} else if versionCount > 0 {
		activeField = fmt.Sprintf("none active (%d published)", versionCount)
	}
	draftField := "none"
	if hasDraft {
		draftField = "saved " + stamp(draft["updated_at"])
	}

	// The description prints under the title, so the frontmatter skips it.
	fields := append(withoutField(referenceFields(ref), "description"), api.parentField(ref),
		field{"active_version", activeField},
		field{"draft", draftField},
		field{"tasks_run", scalar(record["task_count"])},
	)
	t.frontmatter(fields...)
	t.gap()
	t.line("# %s", scalar(record["name"]))
	if description := markdownBody(scalar(record["description"])); description != "" {
		t.gap()
		t.block(description)
	}

	t.gap()
	t.line("## Triggers")
	t.gap()
	id := scalar(ref["id"])
	triggers := asList(definition["triggers"])
	disabled := scalar(record["disabled_at"]) != ""
	// `wallfacer run` refuses an archived playbook and one with nothing
	// published, so only a runnable playbook is told it can be run.
	archived := scalar(ref["state"]) == "archived"
	runnable := active != nil && !archived
	switch {
	case archived:
		t.line("Archived: nothing starts this playbook and it cannot be run. `wallfacer handbook restore-playbook %s` brings it back, disabled.", id)
		t.gap()
	case active == nil && versionCount > 0:
		t.line("No version is active, so nothing starts this playbook and it cannot be run.")
	case active == nil:
		t.line("Nothing published yet, so nothing starts this playbook and it cannot be run.")
	case len(triggers) == 0:
		t.line("None. This playbook starts only by hand: `wallfacer run %s`.", id)
	case disabled:
		t.line("Disabled: these triggers are switched off, so nothing starts this playbook on its own. `wallfacer run %s` still starts it by hand.", id)
		t.gap()
	}
	for _, item := range triggers {
		writeTrigger(t, asMap(item))
	}

	steps := asList(definition["steps"])
	t.gap()
	switch {
	case active == nil && versionCount > 0:
		t.line("## Steps")
		t.gap()
		if hasDraft {
			t.line("None active; %s published, and a draft is saved.", plural(versionCount, "version", "versions"))
		} else {
			t.line("None active; %s published. Publishing one again makes it active.", plural(versionCount, "version", "versions"))
		}
	case active == nil && hasDraft:
		t.line("## Steps")
		t.gap()
		t.line("None published yet; a draft is saved.")
	case active == nil:
		t.line("## Steps")
		t.gap()
		t.line("Nothing published and no draft saved; save one with `wallfacer handbook save-draft %s --definition-file <file>`.", id)
	default:
		t.line("## Steps (v%s)", scalar(active["version"]))
	}
	for i, item := range steps {
		step := asMap(item)
		t.gap()
		t.line("### %d. %s", i+1, scalar(step["title"]))
		t.line("%s", stepMeta(step))
		if content := markdownBody(scalar(step["content"])); content != "" {
			t.gap()
			t.block(content)
		}
	}

	// The editable-YAML line is the `version` command, labelled with what it
	// prints.
	followUp := withoutKeys(p["follow_up"], "read", "version")
	switch {
	case runnable:
		followUp["run"] = fmt.Sprintf("wallfacer run %s", id)
	case archived:
		followUp["restore"] = fmt.Sprintf("wallfacer handbook restore-playbook %s", id)
	case hasDraft:
		followUp["publish"] = fmt.Sprintf("wallfacer handbook publish %s", id)
	}
	if active == nil && len(nextLines(followUp, nil)) == 0 {
		return
	}
	t.gap()
	t.line("---")
	if active != nil {
		t.line("Full definition as editable YAML: wallfacer handbook version %s", id)
	}
	t.next(followUp, api.annotator(id))
}

// triggerLead is the field that says when a trigger fires, shown first.
var triggerLead = []string{"run_when", "prose", "cron"}

func writeTrigger(t *textOut, trigger map[string]interface{}) {
	lead := ""
	used := map[string]bool{"id": true, "kind": true}
	for _, key := range triggerLead {
		if value := oneLine(scalar(trigger[key])); value != "" {
			lead = value
			used[key] = true
			break
		}
	}
	t.line("- `%s` (%s): %s", scalar(trigger["id"]), scalar(trigger["kind"]), lead)
	for _, key := range sortedMapKeys(trigger) {
		if used[key] || trigger[key] == nil {
			continue
		}
		t.line("  %s: %s", key, configValue(trigger[key]))
	}
}

// stepConfigLead orders the step settings a reader scans for first.
var stepConfigLead = []string{"actor", "vendor", "model", "on_success", "on_failure", "max_attempts"}

func stepMeta(step map[string]interface{}) string {
	parts := []string{"`" + scalar(step["id"]) + "`", scalar(step["kind"])}
	config := asMap(step["config"])
	for _, key := range orderedKeys(config, stepConfigLead) {
		if config[key] == nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", key, configValue(config[key])))
	}
	return strings.Join(parts, " · ")
}

// configValue summarizes a setting on one line. Structured values (an output
// schema, a list of approvers) are named rather than dumped; the full
// definition is one `handbook version` away.
func configValue(value interface{}) string {
	switch v := value.(type) {
	case map[string]interface{}:
		if properties := asMap(v["properties"]); properties != nil {
			summary := "fields " + strings.Join(sortedMapKeys(properties), ", ")
			if required := asList(v["required"]); len(required) > 0 {
				var names []string
				for _, r := range required {
					names = append(names, scalar(r))
				}
				summary += " (required: " + strings.Join(names, ", ") + ")"
			}
			return summary
		}
		return "{" + strings.Join(sortedMapKeys(v), ", ") + "}"
	case []interface{}:
		var parts []string
		for _, item := range v {
			if !isScalar(item) {
				return plural(len(v), "entry", "entries")
			}
			parts = append(parts, scalar(item))
		}
		return strings.Join(parts, ", ")
	}
	return oneLine(scalar(value))
}

func renderHandbookResolve(api *handbookAPI) textRenderer {
	return func(t *textOut, p map[string]interface{}) {
		ref := asMap(p["data"])
		fields := append(referenceFields(ref), api.parentField(ref), field{"resolved_from", scalar(ref["resolved_from"])})
		t.frontmatter(fields...)
		t.next(p["follow_up"], api.annotator(scalar(ref["id"])))
	}
}

// page revisions

func renderHandbookRevisions(api *handbookAPI) textRenderer {
	return func(t *textOut, p map[string]interface{}) {
		ref := asMap(p["reference"])
		items := asList(p["data"])
		t.line("Revisions of %q (%s), newest first.", scalar(ref["title"]), scalar(ref["id"]))
		t.line("Saves by one author within one editing session share a revision.")
		t.gap()
		if len(items) == 0 {
			t.line("No revisions recorded.")
		}
		for _, item := range items {
			revision := asMap(item)
			line := fmt.Sprintf("- %s · %s · user %s", scalar(revision["id"]), stamp(revision["updated_at"]), scalar(revision["created_by"]))
			if title := scalar(revision["title"]); title != "" && title != scalar(ref["title"]) {
				line += fmt.Sprintf(" · titled %q", title)
			}
			t.text(line)
		}
		writePageOf(t, asMap(p["pagination"]), "revisions")
		t.next(dropSpentNextPage(p["follow_up"], asMap(p["pagination"])), api.annotator(scalar(ref["id"])))
	}
}

func renderHandbookRevision(api *handbookAPI) textRenderer {
	return func(t *textOut, p map[string]interface{}) {
		ref := asMap(p["reference"])
		revision := asMap(p["data"])
		t.frontmatter(
			field{"type", "page revision"},
			field{"revision_id", scalar(revision["id"])},
			field{"page_id", scalar(ref["id"])},
			field{"path", scalar(ref["path"])},
			field{"saved_at", stamp(revision["updated_at"])},
			field{"saved_by", "user " + scalar(revision["created_by"])},
			field{"description", oneLine(scalar(revision["description"]))},
		)
		t.gap()
		t.line("# %s", scalar(revision["title"]))
		t.gap()
		if body := markdownBody(scalar(revision["body"])); body != "" {
			t.block(body)
		} else {
			t.line("_This revision has no body._")
		}
		t.gap()
		t.line("---")
		t.next(p["follow_up"], api.annotator(scalar(ref["id"])))
	}
}

// writePageOf reports where a paginated listing is, from Laravel's meta block.
func writePageOf(t *textOut, pagination map[string]interface{}, noun string) {
	meta := asMap(pagination["meta"])
	if meta == nil {
		return
	}
	t.gap()
	t.line("Page %s of %s (%s %s).", scalar(meta["current_page"]), scalar(meta["last_page"]), scalar(meta["total"]), noun)
}

// playbook versions and drafts

func renderHandbookVersions(api *handbookAPI) textRenderer {
	return func(t *textOut, p map[string]interface{}) {
		ref := asMap(p["reference"])
		activeVersion := scalar(asMap(ref["active_version"])["version"])
		t.line("Published versions of %q (%s), newest first.", scalar(ref["title"]), scalar(ref["id"]))
		t.line("A task runs the version that was active when it started.")
		t.gap()
		items := asList(p["data"])
		if len(items) == 0 {
			t.line("Nothing published yet.")
		}
		for _, item := range items {
			version := asMap(item)
			number := scalar(version["version"])
			line := "- v" + number
			if number == activeVersion {
				line += " (active)"
			}
			line += fmt.Sprintf(" · %s · %s · user %s", scalar(version["id"]), stamp(version["created_at"]), scalar(version["created_by"]))
			if notes := oneLine(scalar(version["notes"])); notes != "" {
				line += " — " + notes
			}
			t.text(line)
		}
		writePageOf(t, asMap(p["pagination"]), "versions")
		followUp := dropSpentNextPage(p["follow_up"], asMap(p["pagination"]))
		// When the newest version listed is the active one, `version` already
		// reads it.
		if activeVersion != "" && followUp["version"] == fmt.Sprintf("wallfacer handbook version %s %s", scalar(ref["id"]), activeVersion) {
			delete(followUp, "active")
		}
		t.next(followUp, api.annotator(scalar(ref["id"])))
	}
}

// renderHandbookVersion prints a published definition as a YAML document,
// with everything else in comments, so the output saves to a file, edits, and
// goes back with `handbook save-draft --definition-file`.
func renderHandbookVersion(api *handbookAPI) textRenderer {
	return func(t *textOut, p map[string]interface{}) {
		ref := asMap(p["reference"])
		version := asMap(p["data"])
		id := scalar(ref["id"])
		number := scalar(version["version"])
		state := ""
		if number == scalar(asMap(ref["active_version"])["version"]) {
			state = " (active)"
		}
		t.comments(
			fmt.Sprintf("Playbook %q · version %s%s · %s", scalar(ref["title"]), number, state, id),
			fmt.Sprintf("Published %s by user %s.", stamp(version["created_at"]), scalar(version["created_by"])),
		)
		if notes := oneLine(scalar(version["notes"])); notes != "" {
			t.comments("Notes: " + notes)
		}
		t.comments(
			"",
			"This is the full definition, as YAML. To change the playbook, save it to a file, edit it, then:",
			fmt.Sprintf("  wallfacer handbook save-draft %s --definition-file <file>", id),
			fmt.Sprintf("  wallfacer handbook diff-draft %s", id),
			fmt.Sprintf("  wallfacer handbook publish %s", id),
		)
		t.gap()
		t.block(definitionYAML(version["definition"]))
		writeNextAsComments(t, p["follow_up"], api.annotator(id))
	}
}

// renderHandbookDraft takes the playbook's version count from the command,
// since the reference may come from a tree node, which carries none.
func renderHandbookDraft(api *handbookAPI, versionCount int) textRenderer {
	return func(t *textOut, p map[string]interface{}) {
		ref := asMap(p["reference"])
		data := asMap(p["data"])
		id := scalar(ref["id"])
		activeVersion := scalar(asMap(ref["active_version"])["version"])
		running := "Nothing is published yet."
		switch {
		case activeVersion != "":
			running = fmt.Sprintf("Tasks still run the active version, v%s.", activeVersion)
		case versionCount > 0:
			running = fmt.Sprintf("No version is active (%s published).", plural(versionCount, "version", "versions"))
		}

		if present, _ := data["present"].(bool); !present {
			switch {
			case activeVersion != "":
				t.comments(
					fmt.Sprintf("Playbook %q (%s) has no saved draft. %s", scalar(ref["title"]), id, running),
					fmt.Sprintf("Start one from the active definition: wallfacer handbook version %s > draft.yaml", id),
				)
			case versionCount > 0:
				t.comments(
					fmt.Sprintf("Playbook %q (%s) has no saved draft. %s", scalar(ref["title"]), id, running),
					fmt.Sprintf("Start one from a published version: wallfacer handbook version %s <n> > draft.yaml", id),
				)
				writeNextAsComments(t, p["follow_up"], api.annotator(id))
			default:
				t.comments(
					fmt.Sprintf("Playbook %q (%s): nothing published and no draft saved.", scalar(ref["title"]), id),
					fmt.Sprintf("Save one with: wallfacer handbook save-draft %s --definition-file <file>", id),
				)
			}
			return
		}

		draft := asMap(data["draft"])
		t.comments(
			fmt.Sprintf("Playbook %q · unpublished draft · %s", scalar(ref["title"]), id),
			fmt.Sprintf("Saved %s by user %s. %s", stamp(draft["updated_at"]), scalar(draft["updated_by"]), running),
			"Drafts are not validated until they are published.",
			"",
		)
		if activeVersion != "" {
			t.comments(
				fmt.Sprintf("Compare with the active version: wallfacer handbook diff-draft %s", id),
				fmt.Sprintf("Publish:                         wallfacer handbook publish %s", id),
			)
		} else {
			t.comments(fmt.Sprintf("Publish: wallfacer handbook publish %s", id))
		}
		t.gap()
		t.block(definitionYAML(draft["definition"]))
		writeNextAsComments(t, p["follow_up"], api.annotator(id))
	}
}

func writeNextAsComments(t *textOut, followUp interface{}, annotate func(string) string) {
	rows := nextLines(followUp, annotate)
	if len(rows) == 0 {
		return
	}
	t.gap()
	t.comments("Next:")
	for _, row := range rows {
		t.comments(row)
	}
}

// renderHandbookDiff shows two published versions as a unified diff of their
// YAML definitions.
func renderHandbookDiff(api *handbookAPI) textRenderer {
	return func(t *textOut, p map[string]interface{}) {
		ref := asMap(p["reference"])
		data := asMap(p["data"])
		left := "v" + scalar(asMap(data["left"])["version"])
		right := "v" + scalar(asMap(data["right"])["version"])
		t.line("Playbook %q (%s): %s → %s. Reading a diff publishes nothing.", scalar(ref["title"]), scalar(ref["id"]), left, right)
		t.gap()
		writeUnifiedDiff(t, definitionYAML(data["left_definition"]), definitionYAML(data["right_definition"]), left, right)
		t.next(p["follow_up"], api.annotator(scalar(ref["id"])))
	}
}

// renderHandbookDiffDraft shows the saved draft against the active version as
// a unified diff. The two definitions come from the command, since the payload
// carries the field-by-field change list rather than both documents.
func renderHandbookDiffDraft(api *handbookAPI, activeDefinition, draftDefinition interface{}) textRenderer {
	return func(t *textOut, p map[string]interface{}) {
		ref := asMap(p["reference"])
		data := asMap(p["data"])
		activeLabel := "active"
		if version := scalar(asMap(data["active_version"])["version"]); version != "" {
			activeLabel = "active v" + version
		}
		changes := asList(data["changes"])
		if identical, _ := data["identical"].(bool); identical {
			t.line("The draft of %q matches %s exactly.", scalar(ref["title"]), activeLabel)
		} else {
			t.line("Draft of %q against %s: %s. Nothing is published.", scalar(ref["title"]), activeLabel, plural(len(changes), "field changed", "fields changed"))
		}
		if note := scalar(data["note"]); note != "" {
			t.line("%s.", strings.TrimSuffix(note, "."))
		}
		t.gap()
		writeUnifiedDiff(t, definitionYAML(activeDefinition), definitionYAML(draftDefinition), activeLabel, "draft")
		t.next(p["follow_up"], api.annotator(scalar(ref["id"])))
	}
}

// definitionYAML renders a playbook definition with its keys in reading
// order: what it is, what starts it, then what it does, and within a step its
// identity before its settings and prose.
func definitionYAML(definition interface{}) string {
	if definition == nil {
		return ""
	}
	ordered := orderDefinition(definition)
	encoded, err := yaml.Marshal(ordered)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func orderDefinition(definition interface{}) interface{} {
	top := asMap(definition)
	if top == nil {
		return definition
	}
	out := yaml.MapSlice{}
	for _, key := range orderedKeys(top, []string{"description", "format_version", "triggers", "steps"}) {
		value := top[key]
		switch key {
		case "triggers":
			value = orderEach(value, []string{"id", "kind", "run_when", "skip_when", "prose", "cron", "timezone"}, nil)
		case "steps":
			value = orderEach(value, []string{"id", "kind", "title", "config", "content"}, map[string][]string{"config": stepConfigLead})
		}
		out = append(out, yaml.MapItem{Key: key, Value: value})
	}
	return out
}

func orderEach(value interface{}, lead []string, nested map[string][]string) interface{} {
	items := asList(value)
	if items == nil {
		return value
	}
	out := make([]interface{}, 0, len(items))
	for _, item := range items {
		m := asMap(item)
		if m == nil {
			out = append(out, item)
			continue
		}
		ordered := yaml.MapSlice{}
		for _, key := range orderedKeys(m, lead) {
			v := m[key]
			if nestedLead, ok := nested[key]; ok && asMap(v) != nil {
				inner := yaml.MapSlice{}
				for _, k := range orderedKeys(asMap(v), nestedLead) {
					inner = append(inner, yaml.MapItem{Key: k, Value: asMap(v)[k]})
				}
				v = inner
			}
			ordered = append(ordered, yaml.MapItem{Key: key, Value: v})
		}
		out = append(out, ordered)
	}
	return out
}

// orderedKeys returns m's keys with lead first (those present), then the rest
// alphabetically.
func orderedKeys(m map[string]interface{}, lead []string) []string {
	seen := map[string]bool{}
	keys := []string{}
	for _, key := range lead {
		if _, ok := m[key]; ok {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	for _, key := range sortedMapKeys(m) {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	return keys
}

func sortedMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// writes

// writeFactSkip lists record fields a write summary leaves out: identity
// already in the headline, bookkeeping, and bodies the caller just sent.
var writeFactSkip = map[string]bool{
	"id": true, "account_id": true, "title": true, "name": true, "body": true,
	"description": true, "created_at": true, "created_by": true, "updated_by": true,
	"deleted_at": true, "pipeline_id": true, "playbook": true, "definition": true,
	"note": true, "disabled_note": true, "has_body": true, "linked_page_ids": true,
}

// renderHandbookWrite summarizes a write: what changed, where the entry is
// now, what the server said about it, and the reads that show the result.
func renderHandbookWrite(api *handbookAPI, verb string) textRenderer {
	return func(t *textOut, p map[string]interface{}) {
		ref := asMap(p["reference"])
		data := asMap(p["data"])
		title := scalar(ref["title"])
		if title == "" {
			title = scalar(data["title"])
		}
		t.line("%s %s %q · %s", verb, scalar(ref["type"]), title, scalar(ref["id"]))
		if path := scalar(ref["path"]); path != "" {
			t.line("path: %s", path)
		}
		if state := scalar(ref["state"]); state != "" && state != "active" {
			t.line("state: %s", state)
		}
		for _, key := range sortedMapKeys(data) {
			if writeFactSkip[key] || data[key] == nil {
				continue
			}
			switch key {
			case "parent_page_id":
				parent := api.parentField(map[string]interface{}{"parent_page_id": data[key]})
				t.line("parent: %s", parent.value)
			case "updated_at":
				t.line("updated_at: %s", stamp(data[key]))
			default:
				writeFact(t, key, data[key])
			}
		}
		for _, notes := range []interface{}{data["note"], data["disabled_note"], p["note"]} {
			if note := scalar(notes); note != "" {
				t.gap()
				t.line("%s", note)
			}
		}
		// `tree` is the whole handbook, not something this write reaches.
		t.next(withoutKeys(p["follow_up"], "tree"), api.annotator(scalar(ref["id"])))
	}
}

func writeFact(t *textOut, key string, value interface{}) {
	switch v := value.(type) {
	case map[string]interface{}:
		switch {
		case v["version"] != nil:
			t.line("%s: v%s", key, scalar(v["version"]))
		case v["present"] != nil:
			if present, _ := v["present"].(bool); present {
				t.line("%s: present", key)
			} else {
				t.line("%s: none", key)
			}
		default:
			var parts []string
			for _, k := range sortedMapKeys(v) {
				if writeFactSkip[k] || !isScalar(v[k]) || v[k] == nil {
					continue
				}
				parts = append(parts, fmt.Sprintf("%s %s", k, scalar(v[k])))
			}
			if len(parts) > 0 {
				t.line("%s: %s", key, strings.Join(parts, ", "))
			}
		}
	case []interface{}:
		if len(v) == 0 {
			t.line("%s: none", key)
			return
		}
		if isScalar(v[0]) {
			t.line("%s: %s", key, configValue(v))
			return
		}
		t.line("%s:", key)
		for _, item := range v {
			entry := asMap(item)
			t.line("  - %s (%s) · %s", scalar(entry["title"]), scalar(entry["type"]), scalar(entry["id"]))
		}
	default:
		t.line("%s: %s", key, oneLine(scalar(value)))
	}
}

func renderHandbookReorder(parentLabel string) textRenderer {
	return func(t *textOut, p map[string]interface{}) {
		children := asList(p["children"])
		t.line("Reordered %s under %s:", plural(len(children), "entry", "entries"), parentLabel)
		for i, item := range children {
			child := asMap(item)
			t.line("  position %d: %s (%s) · %s", i, scalar(child["title"]), scalar(child["type"]), scalar(child["id"]))
		}
		if note := scalar(p["note"]); note != "" {
			t.gap()
			t.line("%s", note)
		}
	}
}

// writeUnifiedDiff prints a unified diff of a and b with three lines of
// context, in the standard format patch and git apply read.
func writeUnifiedDiff(t *textOut, a, b, aLabel, bLabel string) {
	if a == b {
		t.line("(no differences)")
		return
	}
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		// SplitLines terminates the last line itself, so a trailing newline
		// would otherwise come back as an extra empty line.
		A:        difflib.SplitLines(strings.TrimSuffix(a, "\n")),
		B:        difflib.SplitLines(strings.TrimSuffix(b, "\n")),
		FromFile: aLabel,
		ToFile:   bLabel,
		Context:  3,
	})
	if err != nil {
		t.line("(could not compute the diff: %v)", err)
		return
	}
	t.block(diff)
}
