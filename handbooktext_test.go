package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// captureText runs a command body in the default text format and returns what
// it printed.
func captureText(t *testing.T, run func() error) string {
	t.Helper()

	viper.Set("output-format", formatText)
	defer viper.Set("output-format", "")

	var buf bytes.Buffer
	previous := cli.Stdout
	cli.Stdout = &buf
	defer func() { cli.Stdout = previous }()

	if err := run(); err != nil {
		t.Fatalf("command failed: %v", err)
	}
	return buf.String()
}

func assertContains(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q\n%s", want, out)
		}
	}
}

func TestOutputFormatResolution(t *testing.T) {
	defer viper.Set("output-format", "")
	defer viper.Set("query", "")

	for _, tc := range []struct {
		format, query, want string
	}{
		{"", "", formatText},
		{"text", "", formatText},
		{"json", "", formatJSON},
		{"YAML", "", formatYAML},
		{"", "data.id", formatJSON},
		{"text", "data.id", formatJSON},
		{"yaml", "data.id", formatYAML},
	} {
		viper.Set("output-format", tc.format)
		viper.Set("query", tc.query)
		got, err := outputFormat()
		if err != nil {
			t.Fatalf("-o %q -q %q: %v", tc.format, tc.query, err)
		}
		if got != tc.want {
			t.Errorf("-o %q -q %q resolved to %s, want %s", tc.format, tc.query, got, tc.want)
		}
	}

	viper.Set("query", "")
	viper.Set("output-format", "markdown")
	if _, err := outputFormat(); err == nil {
		t.Error("an unknown format should be refused, not silently printed as text")
	}
}

// With nothing asked for, a handbook command prints text, and the same command
// with -o json prints the JSON payload. This is the contract scripts rely on.
func TestHandbookDefaultsToTextAndJSONIsOptIn(t *testing.T) {
	fixture := newHandbookFixture(t)

	viper.Set("output-format", "")
	var buf bytes.Buffer
	previous := cli.Stdout
	cli.Stdout = &buf
	err := runHandbookRead(fixture.api(), pageBuildID, "")
	cli.Stdout = previous
	if err != nil {
		t.Fatal(err)
	}
	if json.Valid(buf.Bytes()) {
		t.Fatalf("default output is JSON, want text\n%s", buf.String())
	}
	assertContains(t, buf.String(), "---\ntype: page\n")

	output := capture(t, func() error { return runHandbookRead(fixture.api(), pageBuildID, "") })
	if output["data"].(map[string]interface{})["body"] == nil {
		t.Errorf("-o json lost the page body: %v", output)
	}
}

func TestTreeTextIsAnOutline(t *testing.T) {
	fixture := newHandbookFixture(t)

	out := captureText(t, func() error { return runHandbookTree(fixture.api(), "", 0) })

	assertContains(t, out,
		"- Engineering · page · "+pageEngineeringID+" — How we build and ship.",
		"  - Build · page · "+pageBuildID,
		"  - Build · playbook v2, draft saved · "+playbookBuildID,
	)
	if strings.Contains(out, `"children"`) {
		t.Errorf("text tree leaked JSON:\n%s", out)
	}
}

func TestTreeUnderAndDepth(t *testing.T) {
	fixture := newHandbookFixture(t)

	out := captureText(t, func() error { return runHandbookTree(fixture.api(), "Engineering", 0) })
	assertContains(t, out, "Handbook under Engineering:", "- Build · page · "+pageBuildID)
	if strings.Contains(out, "Product") {
		t.Errorf("--under Engineering printed another branch:\n%s", out)
	}

	shallow := captureText(t, func() error { return runHandbookTree(fixture.api(), "", 1) })
	assertContains(t, shallow, "- Engineering · page · "+pageEngineeringID, "below]", "raise --depth")
	if strings.Contains(shallow, "  - Build") {
		t.Errorf("--depth 1 printed a second level:\n%s", shallow)
	}

	output := capture(t, func() error { return runHandbookTree(fixture.api(), "", 1) })
	for _, item := range output["data"].(map[string]interface{})["tree"].([]interface{}) {
		node := item.(map[string]interface{})
		if len(node["children"].([]interface{})) != 0 {
			t.Errorf("-o json --depth 1 kept children under %v", node["title"])
		}
	}
}

func TestPageReadTextIsMarkdownWithFrontmatter(t *testing.T) {
	fixture := newHandbookFixture(t)

	out := captureText(t, func() error { return runHandbookRead(fixture.api(), "Engineering/Build", kindPage) })

	assertContains(t, out,
		"---\ntype: page\nid: "+pageBuildID+"\n",
		"path: Engineering/Build\n",
		"parent: Engineering · "+pageEngineeringID+"\n",
		"# Build\n\nHow a change reaches the product: issue, pull request, review, merge.\n",
		"wallfacer handbook read "+pageBuildID+" --body > page.md",
		"wallfacer handbook update "+pageBuildID+" --body-file page.md",
	)
	// The parent is labelled with its title; the page itself is not offered
	// as a thing to read next.
	assertContains(t, out, "wallfacer handbook read "+pageEngineeringID+"  # Engineering (page)")
	if strings.Contains(out, "read       wallfacer handbook read "+pageBuildID+"\n") {
		t.Errorf("a page read offered to read itself:\n%s", out)
	}
}

func TestReadBodyPrintsOnlyTheBody(t *testing.T) {
	fixture := newHandbookFixture(t)

	for _, format := range []string{formatText, formatJSON} {
		viper.Set("output-format", format)
		var buf bytes.Buffer
		previous := cli.Stdout
		cli.Stdout = &buf
		err := runHandbookReadBody(fixture.api(), pageBuildID, "")
		cli.Stdout = previous
		viper.Set("output-format", "")
		if err != nil {
			t.Fatal(err)
		}
		if got, want := buf.String(), "How a change reaches the product: issue, pull request, review, merge.\n"; got != want {
			t.Errorf("-o %s --body printed %q, want %q", format, got, want)
		}
	}

	if err := runHandbookReadBody(fixture.api(), playbookBuildID, ""); err == nil || !strings.Contains(err.Error(), "handbook version") {
		t.Errorf("--body on a playbook should point at handbook version, got %v", err)
	}
}

// "Build" names both a page and a playbook. --body reads a page, so the name
// resolves to the page instead of reporting the ambiguity.
func TestReadBodyResolvesAsAPage(t *testing.T) {
	fixture := newHandbookFixture(t)

	out := captureText(t, func() error { return runHandbookReadBody(fixture.api(), "Build", "") })
	if want := "How a change reaches the product: issue, pull request, review, merge.\n"; out != want {
		t.Errorf("read Build --body printed %q, want the page body %q", out, want)
	}

	err := runHandbookReadBody(fixture.api(), "Build", kindPlaybook)
	if err == nil || !strings.Contains(err.Error(), "--type playbook") {
		t.Errorf("--body --type playbook should be refused, got %v", err)
	}
}

func TestPlaybookReadTextShowsTriggersAndSteps(t *testing.T) {
	fixture := newHandbookFixture(t)

	out := captureText(t, func() error { return runHandbookRead(fixture.api(), playbookBuildID, "") })

	assertContains(t, out,
		"type: playbook\n",
		"active_version: v2 of 2\n",
		"draft: saved 2026-09-10T00:00:00Z\n",
		"# Build\n\nImplement an assigned issue.\n",
		"## Triggers\n\nNone. This playbook starts only by hand",
		"## Steps (v2)\n\n### 1. Implement the issue\n`implement` · ai\n\nRead the issue and implement it.\n",
		"wallfacer handbook version "+playbookBuildID,
		"wallfacer handbook read "+pageBuildID+"  # Build (page)",
	)
}

// The text form of a version is a YAML document that loads back as the same
// definition, which is what lets `version > file`, edit, `save-draft
// --definition-file file` work without hand-unwrapping anything.
func TestVersionTextRoundTripsIntoSaveDraft(t *testing.T) {
	fixture := newHandbookFixture(t)

	out := captureText(t, func() error { return runHandbookVersion(fixture.api(), playbookBuildID, "2") })
	assertContains(t, out, "# Playbook \"Build\" · version 2 (active)", "save-draft "+playbookBuildID+" --definition-file")

	path := filepath.Join(t.TempDir(), "playbook.yaml")
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadDefinition(path)
	if err != nil {
		t.Fatalf("the printed version does not load as a definition: %v\n%s", err, out)
	}

	var record map[string]interface{}
	if err := json.Unmarshal([]byte(playbookVersionRecordJSON), &record); err != nil {
		t.Fatal(err)
	}
	want := record["definition"]
	if !reflect.DeepEqual(normalizeNumbers(loaded), normalizeNumbers(want)) {
		t.Errorf("round trip changed the definition\n got: %v\nwant: %v", loaded, want)
	}
}

// normalizeNumbers lets a YAML-decoded int compare equal to a JSON-decoded
// float64.
func normalizeNumbers(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		out := map[string]interface{}{}
		for key, item := range v {
			out[key] = normalizeNumbers(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(v))
		for i, item := range v {
			out[i] = normalizeNumbers(item)
		}
		return out
	case int:
		return float64(v)
	}
	return value
}

func TestSearchRanksTitleMatchesBeforeBodyMatches(t *testing.T) {
	fixture := newHandbookFixture(t)

	// "review" is in the Build page's body, and in the title of both Review
	// pages. The API lists Build first, so only ranking puts the title hits
	// ahead of it.
	output := capture(t, func() error { return runHandbookSearch(fixture.api(), "review", "", 20, 20) })
	data := output["data"].([]interface{})
	if len(data) < 3 {
		t.Fatalf("want at least three matches, got %v", data)
	}
	if first := data[0].(map[string]interface{}); first["id"] != pageReviewEngID {
		t.Errorf("first match is %v, want the first page titled Review", first["id"])
	}
	last := data[len(data)-1].(map[string]interface{})
	if last["id"] != pageBuildID {
		t.Fatalf("the body-only match should rank last, got %v", last["id"])
	}
	if snippet, _ := last["snippet"].(string); !strings.Contains(snippet, "review") {
		t.Errorf("a body match should carry a snippet around the match, got %q", snippet)
	}

	// The limit applies after ranking.
	limited := capture(t, func() error { return runHandbookSearch(fixture.api(), "review", "", 1, 20) })
	if first := limited["data"].([]interface{})[0].(map[string]interface{}); first["id"] != pageReviewEngID {
		t.Errorf("--limit 1 kept %v, want the best match", first["id"])
	}
	if limited["truncated"] != true {
		t.Errorf("--limit 1 with more matches should report truncated, got %v", limited["truncated"])
	}

	out := captureText(t, func() error { return runHandbookSearch(fixture.api(), "review", "", 20, 20) })
	assertContains(t, out, "title matches first", "matched in body: “", "matched in title, description, body: “")
}

// A body hit carries a snippet whatever else matched, not only when the body
// is the best-ranked field.
func TestEveryBodyMatchCarriesASnippet(t *testing.T) {
	fixture := newHandbookFixture(t)

	output := capture(t, func() error { return runHandbookSearch(fixture.api(), "review", "", 20, 20) })
	multiField := false
	for _, item := range output["data"].([]interface{}) {
		entry := item.(map[string]interface{})
		matched := entry["matched_in"].([]interface{})
		bodyMatched := false
		for _, field := range matched {
			bodyMatched = bodyMatched || field == "body"
		}
		if !bodyMatched {
			continue
		}
		if matched[0] != "body" {
			multiField = true
		}
		if snippet, _ := entry["snippet"].(string); snippet == "" {
			t.Errorf("%v matched in %v but carries no snippet", entry["id"], matched)
		}
	}
	if !multiField {
		t.Fatal("the fixture needs a match on the body and a better-ranked field")
	}
}

func TestSnippetAround(t *testing.T) {
	text := strings.Repeat("a ", 50) + "The Needle sits here." + strings.Repeat(" b", 50)
	got := snippetAround(text, "needle", 10)
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") || !strings.Contains(got, "Needle") {
		t.Errorf("snippet = %q", got)
	}
	if snippetAround("short text", "absent", 10) != "" {
		t.Error("no match should give no snippet")
	}
	if got := snippetAround("héllo needle wörld", "needle", 3); !strings.Contains(got, "needle") {
		t.Errorf("multi-byte snippet = %q", got)
	}
}

func TestVersionsTextMarksTheActiveVersion(t *testing.T) {
	fixture := newHandbookFixture(t)

	out := captureText(t, func() error { return runHandbookVersions(fixture.api(), playbookBuildID, 0, 0) })
	assertContains(t, out,
		"- v2 (active) · "+playbookVersionID,
		"— Second cut.",
		"Page 1 of 2 (2 versions).",
		"wallfacer handbook versions "+playbookBuildID+" --page 2",
	)
}

func TestDraftTextIsTheDraftDefinition(t *testing.T) {
	fixture := newHandbookFixture(t)

	out := captureText(t, func() error { return runHandbookDraft(fixture.api(), playbookBuildID) })
	assertContains(t, out,
		"unpublished draft",
		"Tasks still run the active version, v2.",
		"title: Implement the issue, revised",
	)
}

// With an active version and no draft, the draft view still ends with the
// follow-up the JSON carries, written as YAML comments.
func TestDraftTextWithNoDraftEndsWithItsFollowUp(t *testing.T) {
	fixture := newAuthoringFixture(t)

	out := captureText(t, func() error { return runHandbookDraft(fixture.api(), playbookPublishedOnlyID) })
	assertContains(t, out,
		"has no saved draft. Tasks still run the active version, v2.",
		"# Next:",
		"wallfacer handbook version "+playbookPublishedOnlyID+"\n",
	)
}

func TestWriteTextSummarizesTheResult(t *testing.T) {
	fixture := newHandbookFixture(t)

	out := captureText(t, func() error {
		return runHandbookCreate(fixture.api(), handbookEdit{body: map[string]interface{}{"title": "Writing Great PRs", "body": "Lead with the problem."}, parentRef: "Engineering"})
	})
	assertContains(t, out,
		`Created page "Writing Great PRs" · `+pageCreatedID,
		"position: 3",
		"live handbook knowledge",
		"wallfacer handbook revisions "+pageCreatedID,
	)
	if strings.Contains(out, "Lead with the problem.") {
		t.Errorf("the write summary echoed the body back:\n%s", out)
	}
}

func TestUnifiedDiff(t *testing.T) {
	var out textOut
	writeUnifiedDiff(&out, "a\nb\nc\nd\n", "a\nB\nc\nd\ne\n", "v1", "v2")
	want := "--- v1\n+++ v2\n@@ -1,4 +1,5 @@\n a\n-b\n+B\n c\n d\n+e\n"
	if got := out.String(); got != want {
		t.Errorf("diff =\n%s\nwant\n%s", got, want)
	}

	var same textOut
	writeUnifiedDiff(&same, "a\n", "a\n", "x", "y")
	if !strings.Contains(same.String(), "(no differences)") {
		t.Errorf("identical inputs: %s", same.String())
	}
}

// A search that stopped before reading every page must not report a clean
// miss: the match may be on a page it never read.
func TestSearchWithNoMatchesReportsAnIncompleteSweep(t *testing.T) {
	fixture := newHandbookFixture(t)

	out := captureText(t, func() error { return runHandbookSearch(fixture.api(), "zzz-absent", "", 20, 1) })
	assertContains(t, out, `No pages or playbooks match "zzz-absent" in what was searched.`, "--max-pages 2")
	if strings.Contains(out, "Try a shorter") {
		t.Errorf("an incomplete search suggested the query was wrong:\n%s", out)
	}
}

// The search commands a result prints are pasted into a shell, so the query
// comes back single-quoted rather than as a Go string the shell would expand.
func TestSearchCommandsQuoteTheQueryForTheShell(t *testing.T) {
	fixture := newHandbookFixture(t)

	out := captureText(t, func() error { return runHandbookSearch(fixture.api(), "$(true) it's", "", 20, 1) })
	assertContains(t, out, `wallfacer handbook search '$(true) it'\''s' --limit 20 --max-pages 2`)
}

// The continuation commands repeat the search that was run, widening only
// the bound each one is about.
func TestSearchContinuationsKeepTheSearchScope(t *testing.T) {
	fixture := newHandbookFixture(t)

	more := captureText(t, func() error { return runHandbookSearch(fixture.api(), "review", kindPage, 1, 50) })
	assertContains(t, more, "See more: wallfacer handbook search review --type page --limit ")
	assertContains(t, more, " --max-pages 50\n")

	further := captureText(t, func() error { return runHandbookSearch(fixture.api(), "zzz-absent", kindPage, 5, 1) })
	assertContains(t, further, "Search further: wallfacer handbook search zzz-absent --type page --limit 5 --max-pages 2\n")
}

func TestShellQuote(t *testing.T) {
	for arg, want := range map[string]string{
		"triage":            "triage",
		"Engineering/Build": "Engineering/Build",
		"pull request":      "'pull request'",
		"$HOME":             "'$HOME'",
		"it's":              `'it'\''s'`,
		"":                  "''",
	} {
		if got := shellQuote(arg); got != want {
			t.Errorf("shellQuote(%q) = %s, want %s", arg, got, want)
		}
	}
}

// Leading indentation is markdown: a body that opens with an indented code
// block keeps it, and only the blank lines around the body are dropped.
func TestMarkdownBodyKeepsLeadingIndentation(t *testing.T) {
	for body, want := range map[string]string{
		"    go test ./...\n\nRun it first.\n": "    go test ./...\n\nRun it first.",
		"\n\n  \n    indented\n\n":             "    indented",
		"plain":                                "plain",
		" \n\t\n":                              "",
	} {
		if got := markdownBody(body); got != want {
			t.Errorf("markdownBody(%q) = %q, want %q", body, got, want)
		}
	}
}

func TestPlaybookReadOffersRunOnlyWhenRunnable(t *testing.T) {
	fixture := newHandbookFixture(t)

	published := captureText(t, func() error { return runHandbookRead(fixture.api(), playbookBuildID, "") })
	assertContains(t, published, "wallfacer run "+playbookBuildID)

	archived := captureText(t, func() error { return runHandbookRead(fixture.api(), playbookArchivedID, kindPlaybook) })
	assertContains(t, archived, "Archived: nothing starts this playbook and it cannot be run.", "wallfacer handbook restore-playbook "+playbookArchivedID)
	if strings.Contains(archived, "wallfacer run ") {
		t.Errorf("an archived playbook was offered a run:\n%s", archived)
	}
}

// An unpublished playbook has no active version to read and, without a draft,
// nothing to publish. Every result about one is built through the real
// commands, follow_up included, and none of them prints a command that fails.
func TestUnpublishedPlaybookOffersNoCommandThatFails(t *testing.T) {
	fixture := newAuthoringFixture(t)
	api := func() *handbookAPI { return fixture.api() }
	// These IDs are outside the tree, so they are read as playbooks: an
	// untyped ID would also be looked up among deleted pages, which this
	// fixture does not paginate.

	refusesActive := func(t *testing.T, what, out, id string) {
		t.Helper()
		for _, bad := range []string{
			"wallfacer handbook version " + id + "\n",
			"wallfacer handbook version " + id + " ",
			"wallfacer run " + id,
		} {
			if strings.Contains(out, bad) {
				t.Errorf("%s offered %q for a playbook with nothing published:\n%s", what, strings.TrimSpace(bad), out)
			}
		}
	}

	t.Run("with a draft", func(t *testing.T) {
		id := playbookUnpublishedID

		read := captureText(t, func() error { return runHandbookRead(api(), id, kindPlaybook) })
		refusesActive(t, "read", read, id)
		assertContains(t, read,
			"Nothing published yet, so nothing starts this playbook",
			"wallfacer handbook draft "+id,
			"wallfacer handbook publish "+id,
		)
		if strings.Contains(read, "save-draft") {
			t.Errorf("a playbook with a saved draft was told to save one:\n%s", read)
		}

		draft := captureText(t, func() error { return runHandbookDraft(api(), id) })
		refusesActive(t, "draft", draft, id)
		assertContains(t, draft, "Nothing is published yet.", "wallfacer handbook publish "+id)
		if strings.Contains(draft, "diff-draft") {
			t.Errorf("draft offered a comparison with an active version that does not exist:\n%s", draft)
		}

		versions := captureText(t, func() error { return runHandbookVersions(api(), id, 0, 0) })
		refusesActive(t, "versions", versions, id)

		discard := capture(t, func() error { return runPlaybookDiscardDraft(api(), id) })
		if _, ok := discard["follow_up"].(map[string]interface{})["version"]; ok {
			t.Errorf("discard-draft offered the active version of an unpublished playbook: %v", discard["follow_up"])
		}

		followUp := capture(t, func() error { return runHandbookRead(api(), id, kindPlaybook) })["follow_up"].(map[string]interface{})
		for _, key := range []string{"version", "versions"} {
			if _, ok := followUp[key]; ok {
				t.Errorf("-o json follow_up names %q for an unpublished playbook: %v", key, followUp)
			}
		}
		if followUp["draft"] != "wallfacer handbook draft "+id {
			t.Errorf("-o json follow_up should name the saved draft, got %v", followUp)
		}
	})

	t.Run("without a draft", func(t *testing.T) {
		id := playbookEmptyID

		read := captureText(t, func() error { return runHandbookRead(api(), id, kindPlaybook) })
		refusesActive(t, "read", read, id)
		assertContains(t, read, "Nothing published and no draft saved; save one with `wallfacer handbook save-draft "+id+" --definition-file <file>`.")
		for _, bad := range []string{"wallfacer handbook publish", "wallfacer handbook draft ", "wallfacer handbook versions", "Next:"} {
			if strings.Contains(read, bad) {
				t.Errorf("read of an empty playbook printed %q:\n%s", bad, read)
			}
		}

		resolved := capture(t, func() error { return runHandbookResolve(api(), id, kindPlaybook) })
		if followUp := resolved["follow_up"].(map[string]interface{}); len(followUp) != 1 || followUp["read"] == nil {
			t.Errorf("resolve of an empty playbook should name only the read, got %v", followUp)
		}

		draft := captureText(t, func() error { return runHandbookDraft(api(), id) })
		refusesActive(t, "draft", draft, id)
		assertContains(t, draft, "nothing published and no draft saved", "save-draft "+id+" --definition-file <file>")

		versions := capture(t, func() error { return runHandbookVersions(api(), id, 0, 0) })
		if _, ok := versions["follow_up"].(map[string]interface{})["active"]; ok {
			t.Errorf("versions offered the active version of an unpublished playbook: %v", versions["follow_up"])
		}
	})
}

// A version published with --activate=false leaves a playbook with versions and
// none active. It is not "nothing published", and with no draft there is
// nothing to publish as is.
func TestPlaybookWithVersionsButNoneActive(t *testing.T) {
	fixture := newAuthoringFixture(t)
	id := playbookInactiveID

	read := captureText(t, func() error { return runHandbookRead(fixture.api(), id, kindPlaybook) })
	assertContains(t, read,
		"No version is active, so nothing starts this playbook",
		"active_version: none active (1 published)\n",
		"None active; 1 version published. Publishing one again makes it active.",
		"wallfacer handbook versions "+id,
	)
	for _, bad := range []string{"Nothing published", "none published", "no draft saved", "wallfacer run ", "wallfacer handbook draft "} {
		if strings.Contains(read, bad) {
			t.Errorf("read of a playbook with an inactive version printed %q:\n%s", bad, read)
		}
	}

	draft := captureText(t, func() error { return runHandbookDraft(fixture.api(), id) })
	assertContains(t, draft,
		"No version is active (1 version published).",
		"wallfacer handbook version "+id+" <n> > draft.yaml",
		"#   versions  wallfacer handbook versions "+id,
	)
	if strings.Contains(draft, "Nothing is published") || strings.Contains(draft, "nothing published") {
		t.Errorf("draft of a playbook with an inactive version said nothing is published:\n%s", draft)
	}
}

// Playbooks resolved by name come from the tree, whose nodes carry
// active_version and has_draft but no version count; the gating still holds.
func TestUnpublishedPlaybookResolvedThroughTheTree(t *testing.T) {
	fixture := newAuthoringFixture(t)
	fixture.tree = `{"data":{"tree":[
  {"type":"playbook","id":"` + playbookEmptyID + `","title":"Empty Playbook","description":null,"position":0,"disabled_at":null,"has_draft":false,"active_version":null,"task_count":0}
]}}`

	resolved := capture(t, func() error { return runHandbookResolve(fixture.api(), "Empty Playbook", "") })
	if followUp := resolved["follow_up"].(map[string]interface{}); len(followUp) != 1 || followUp["read"] == nil {
		t.Errorf("resolve through the tree should name only the read, got %v", followUp)
	}

	versions := capture(t, func() error { return runHandbookVersions(fixture.api(), "Empty Playbook", 0, 0) })
	if _, ok := versions["follow_up"].(map[string]interface{})["active"]; ok {
		t.Errorf("versions through the tree offered an active version: %v", versions["follow_up"])
	}

	draft := captureText(t, func() error { return runHandbookDraft(fixture.api(), "Empty Playbook") })
	assertContains(t, draft, "nothing published and no draft saved")
	if strings.Contains(draft, "wallfacer handbook version ") {
		t.Errorf("draft through the tree offered an active version:\n%s", draft)
	}
}

// --body resolves as a page, but a name that matches two pages is still
// ambiguous: it must not be reported as the playbook that shares the name.
func TestReadBodyReportsAnAmbiguousPageName(t *testing.T) {
	fixture := newAuthoringFixture(t)
	fixture.tree = `{"data":{"tree":[
  {"type":"page","id":"` + pageEngineeringID + `","title":"Dup","description":null,"position":0,"has_body":true,"children":[]},
  {"type":"page","id":"` + pageProductID + `","title":"Dup","description":null,"position":1,"has_body":true,"children":[]},
  {"type":"playbook","id":"` + playbookBuildID + `","title":"Dup","description":null,"position":2,"disabled_at":null,"has_draft":false,"active_version":null,"task_count":0}
]}}`

	err := runHandbookReadBody(fixture.api(), "Dup", "")
	if _, ok := err.(*ambiguousRefError); !ok {
		t.Errorf("read Dup --body should report the two pages, got %T: %v", err, err)
	}
}

func TestListNextPageKeepsFlags(t *testing.T) {
	fixture := newHandbookFixture(t)

	output := capture(t, func() error { return runHandbookList(fixture.api(), kindPage, 0, 2, true, false) })
	want := "wallfacer handbook list --page <n> --type page --per-page 2 --include-deleted"
	if got := output["follow_up"].(map[string]interface{})["next_page"]; got != want {
		t.Errorf("list next_page = %v, want %q", got, want)
	}
}

// A caller's page size carries into the next-page command; the next page at
// the default size would skip or repeat records.
func TestNextPageKeepsPerPage(t *testing.T) {
	fixture := newHandbookFixture(t)

	versions := captureText(t, func() error { return runHandbookVersions(fixture.api(), playbookBuildID, 0, 1) })
	assertContains(t, versions, "wallfacer handbook versions "+playbookBuildID+" --page 2 --per-page 1")

	revisions := capture(t, func() error { return runHandbookRevisions(fixture.api(), pageBuildID, 0, 10) })
	if got, want := revisions["follow_up"].(map[string]interface{})["next_page"], "wallfacer handbook revisions "+pageBuildID+" --page <n> --per-page 10"; got != want {
		t.Errorf("revisions next_page = %v, want %q", got, want)
	}

	unsized := capture(t, func() error { return runHandbookRevisions(fixture.api(), pageBuildID, 0, 0) })
	if got := unsized["follow_up"].(map[string]interface{})["next_page"]; strings.Contains(got.(string), "--per-page") {
		t.Errorf("an unsized listing added --per-page: %v", got)
	}
}

func TestYAMLScalarQuotesOnlyWhenNeeded(t *testing.T) {
	for in, want := range map[string]string{
		"Build":                "Build",
		"R&D/Engineering":      "R&D/Engineering",
		"Covers: the workflow": `"Covers: the workflow"`,
		"& leading ampersand":  `"& leading ampersand"`,
		"true":                 `"true"`,
		"":                     `""`,
		"ends with colon:":     `"ends with colon:"`,
		"issue#12":             "issue#12",
		"see #12":              `"see #12"`,
		"v4":                   "v4",
		"2026":                 `"2026"`,
		"1e3":                  `"1e3"`,
		"0x1F":                 `"0x1F"`,
		"y":                    `"y"`,
		"2026-09-23":           `"2026-09-23"`,
		"2026-09-23T23:09:08Z": `"2026-09-23T23:09:08Z"`,
	} {
		if got := yamlScalar(in); got != want {
			t.Errorf("yamlScalar(%q) = %s, want %s", in, got, want)
		}
	}
}

// A number decoded from JSON is written as a YAML number; only strings are
// quoted when plain YAML would read them as something else.
func TestFrontmatterKeepsNumbersAndStringsApart(t *testing.T) {
	var out textOut
	out.frontmatter(field{"title", "2026"}, field{"tasks_run", float64(3)})
	assertContains(t, out.b.String(), "title: \"2026\"\n", "tasks_run: 3\n")
}

// Titles are free text and can hold line breaks. Every place one lands in a
// line of output, it is collapsed first, so a pasted Next: row cannot run a
// second command and an entry stays on one line.
func TestTitlesWithLineBreaksStayOnOneLine(t *testing.T) {
	const title = "Parent\ntouch /tmp/pwn #"
	const id = "11111111-1111-4111-8111-111111111111"
	api := &handbookAPI{index: &handbookIndex{byID: map[string]*handbookRef{
		id: {ID: id, Title: title, Type: kindPage},
	}}}

	label := api.annotator("")("wallfacer handbook read " + id)
	if label != "Parent touch /tmp/pwn # (page)" {
		t.Errorf("annotation = %q, want the title on one line", label)
	}
	line := entryLine("", title, map[string]interface{}{"type": kindPage, "id": id})
	if strings.ContainsAny(line, "\r\n") {
		t.Errorf("entry line kept a line break: %q", line)
	}
}

func TestRootHelpGroupsEveryCommand(t *testing.T) {
	root := &cobra.Command{Use: "wallfacer"}
	for _, name := range []string{"chat", "run", "tasks", "handbook", "team", "environments", "auth", "reveries", "pages"} {
		root.AddCommand(&cobra.Command{Use: name, Short: "Manage " + name, Run: func(*cobra.Command, []string) {}})
	}
	configureRootHelp(root)

	var buf bytes.Buffer
	root.SetOutput(&buf)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	assertContains(t, out,
		"Start here:",
		"Words used here:",
		"The API calls it a pipeline.",
		"Work:\n  chat ",
		"Team and handbook:\n",
		"  environments  Computers: the machine definitions agents work on",
		"API resources",
		"  pages, reveries",
		"Docs: https://wallfacer.ai/docs",
	)
	if strings.Contains(out, "Manage tasks") {
		t.Errorf("a curated command kept its generated description:\n%s", out)
	}

	// Subcommands keep the standard help.
	buf.Reset()
	root.SetArgs([]string{"tasks", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Start here:") {
		t.Errorf("a subcommand printed the root help:\n%s", buf.String())
	}
}
