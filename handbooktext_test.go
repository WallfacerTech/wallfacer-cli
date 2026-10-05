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
		err := runHandbookReadBody(fixture.api(), pageBuildID)
		cli.Stdout = previous
		viper.Set("output-format", "")
		if err != nil {
			t.Fatal(err)
		}
		if got, want := buf.String(), "How a change reaches the product: issue, pull request, review, merge.\n"; got != want {
			t.Errorf("-o %s --body printed %q, want %q", format, got, want)
		}
	}

	if err := runHandbookReadBody(fixture.api(), playbookBuildID); err == nil || !strings.Contains(err.Error(), "handbook version") {
		t.Errorf("--body on a playbook should point at handbook version, got %v", err)
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
	assertContains(t, out, "title matches first", "matched in body: “")
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

func TestPlaybookReadOffersRunOnlyWhenRunnable(t *testing.T) {
	fixture := newHandbookFixture(t)

	published := captureText(t, func() error { return runHandbookRead(fixture.api(), playbookBuildID, "") })
	assertContains(t, published, "wallfacer run "+playbookBuildID)

	archived := captureText(t, func() error { return runHandbookRead(fixture.api(), playbookArchivedID, kindPlaybook) })
	assertContains(t, archived, "Archived: nothing starts this playbook and it cannot be run.", "wallfacer handbook restore-playbook "+playbookArchivedID)
	if strings.Contains(archived, "wallfacer run ") {
		t.Errorf("an archived playbook was offered a run:\n%s", archived)
	}

	var unpublished textOut
	renderHandbookRead(&handbookAPI{})(&unpublished, map[string]interface{}{
		"reference": map[string]interface{}{"type": kindPlaybook, "id": playbookBuildID, "title": "Build", "state": "active"},
		"data":      map[string]interface{}{"name": "Build", "draft": map[string]interface{}{"present": true}},
	})
	assertContains(t, unpublished.String(), "Nothing published yet", "wallfacer handbook publish "+playbookBuildID)
	if strings.Contains(unpublished.String(), "wallfacer run ") {
		t.Errorf("an unpublished playbook was offered a run:\n%s", unpublished.String())
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
	} {
		if got := yamlScalar(in); got != want {
			t.Errorf("yamlScalar(%q) = %s, want %s", in, got, want)
		}
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
