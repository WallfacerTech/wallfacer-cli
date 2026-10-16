package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/pkg/errors"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v2"
)

// Output formats. The hand-written product commands (handbook first) print
// text by default: markdown and outlines a person or an agent reads directly.
// JSON and YAML are the structured formats scripts ask for with -o, and a
// --query always gets structured output, since JMESPath projects data rather
// than prose. Generated API commands only know json and yaml.
const (
	formatText = "text"
	formatJSON = "json"
	formatYAML = "yaml"
)

// configureOutputFormat moves the global -o default from json to unset, so a
// command can tell "the caller asked for JSON" from "the caller asked for
// nothing". The generated formatter treats anything other than yaml as JSON,
// so generated commands print exactly what they did before. The few that also
// have a text view print it only when -o text asks; the rest refuse it.
func configureOutputFormat() {
	viper.SetDefault("output-format", "")
	flag := cli.Root.PersistentFlags().Lookup("output-format")
	flag.DefValue = ""
	_ = flag.Value.Set("")
	flag.Usage = "Output format [text, json, yaml]. Handbook commands print text unless asked; API commands print JSON, and only messages list and messages get also print text. --query implies JSON"
	cli.PreRun = requireTextView
}

// outputFormat resolves the format a text-capable command prints in.
func outputFormat() (string, error) {
	format := strings.ToLower(strings.TrimSpace(viper.GetString("output-format")))
	switch format {
	case "", formatText:
		if viper.GetString("query") != "" {
			return formatJSON, nil
		}
		return formatText, nil
	case formatJSON, formatYAML:
		return format, nil
	}
	return "", errors.Errorf("unknown output format %q: use text, json, or yaml", format)
}

// textRenderer writes the text form of a payload. It receives the payload as
// plain decoded JSON, the same shape -o json prints, so the text and the JSON
// are always two views of one result.
type textRenderer func(t *textOut, payload map[string]interface{})

// emitHandbookAs prints a payload as text through render, or as structured
// output when the caller asked for JSON or YAML (or passed --query).
func emitHandbookAs(payload interface{}, render textRenderer) error {
	format, err := outputFormat()
	if err != nil {
		return err
	}
	if format != formatText || render == nil {
		return emitHandbook(payload)
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	decoded := map[string]interface{}{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return err
	}

	t := &textOut{}
	render(t, decoded)
	_, err = io.WriteString(cli.Stdout, t.String())
	return err
}

// textOut accumulates text output, keeping exactly one blank line between
// blocks however the renderers string them together.
type textOut struct {
	b strings.Builder
}

func (t *textOut) String() string {
	return strings.TrimRight(t.b.String(), "\n") + "\n"
}

func (t *textOut) line(format string, args ...interface{}) {
	fmt.Fprintf(&t.b, format, args...)
	t.b.WriteString("\n")
}

// text writes one line verbatim, for content that may itself contain %.
func (t *textOut) text(s string) {
	t.b.WriteString(s)
	t.b.WriteString("\n")
}

// block writes a multi-line chunk as-is (a markdown body, a YAML document).
func (t *textOut) block(text string) {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return
	}
	t.b.WriteString(text)
	t.b.WriteString("\n")
}

// gap ends the current block with one blank line, never two.
func (t *textOut) gap() {
	s := t.b.String()
	if s == "" || strings.HasSuffix(s, "\n\n") {
		return
	}
	if !strings.HasSuffix(s, "\n") {
		t.b.WriteString("\n")
	}
	t.b.WriteString("\n")
}

// field is one frontmatter entry. Empty values are left out.
// A field's value is a string, quoted when plain YAML would read it as
// something else, or a decoded JSON number or bool, written as one.
type field struct {
	key   string
	value interface{}
}

// frontmatter writes a YAML frontmatter block, one key per line, so a page
// reads as an ordinary markdown file and its metadata still parses.
func (t *textOut) frontmatter(fields ...field) {
	t.line("---")
	for _, f := range fields {
		value, isString := f.value.(string)
		if !isString {
			value = scalar(f.value)
		}
		if value == "" {
			continue
		}
		if isString {
			value = yamlScalar(value)
		}
		t.line("%s: %s", f.key, value)
	}
	t.line("---")
}

// comments writes lines as YAML comments, for output that has to stay a valid
// YAML document (a playbook definition someone will edit and save back).
func (t *textOut) comments(lines ...string) {
	for _, l := range lines {
		if l == "" {
			t.line("#")
			continue
		}
		t.line("# %s", l)
	}
}

// followUpOrder puts the commands a reader most likely wants first; anything
// not listed follows alphabetically.
var followUpOrder = []string{
	"read", "current", "body", "edit", "playbook", "parent", "children", "linked_pages",
	"revision", "revisions", "version", "active", "versions", "draft", "run",
	"diff", "save_draft", "publish", "discard", "enable", "restore",
	"a", "b", "next_page", "list", "search", "widen", "resolve", "whole_tree", "tree", "move",
}

// nextLines renders a payload's follow_up commands as aligned "label command"
// rows. annotate may return a short note for a command (the title of the entry
// it reads), which is what turns a list of bare IDs into a choosable list.
func nextLines(followUp interface{}, annotate func(string) string) []string {
	commands, ok := followUp.(map[string]interface{})
	if !ok || len(commands) == 0 {
		return nil
	}

	rank := map[string]int{}
	for i, key := range followUpOrder {
		rank[key] = i
	}
	keys := make([]string, 0, len(commands))
	for key := range commands {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		ri, iok := rank[keys[i]]
		rj, jok := rank[keys[j]]
		switch {
		case iok && jok:
			return ri < rj
		case iok != jok:
			return iok
		}
		return keys[i] < keys[j]
	})

	width := 0
	for _, key := range keys {
		if n := len(strings.ReplaceAll(key, "_", " ")); n > width {
			width = n
		}
	}

	var rows []string
	for _, key := range keys {
		label := strings.ReplaceAll(key, "_", " ")
		var cmds []string
		switch value := commands[key].(type) {
		case string:
			cmds = []string{value}
		case []interface{}:
			for _, item := range value {
				if s, ok := item.(string); ok {
					cmds = append(cmds, s)
				}
			}
		}
		for i, cmd := range cmds {
			shown := label
			if i > 0 {
				shown = ""
			}
			row := fmt.Sprintf("  %-*s  %s", width, shown, cmd)
			if annotate != nil {
				if note := annotate(cmd); note != "" {
					row += "  # " + note
				}
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// next writes the "Next:" section of a text result.
func (t *textOut) next(followUp interface{}, annotate func(string) string) {
	rows := nextLines(followUp, annotate)
	if len(rows) == 0 {
		return
	}
	t.gap()
	t.line("Next:")
	for _, row := range rows {
		t.text(row)
	}
}

// yamlDatePrefix matches the YAML 1.1 timestamp forms. Go's decoder leaves
// them as strings, but other frontmatter readers turn them into dates.
var yamlDatePrefix = regexp.MustCompile(`^[0-9]{4}-[0-9]{1,2}-[0-9]{1,2}`)

// yamlScalar quotes a frontmatter value only when plain YAML would misread
// it, and keeps it on one line either way.
func yamlScalar(value string) string {
	if value == "" {
		return `""`
	}
	needsQuote := strings.ContainsAny(value, "\n\t") ||
		strings.Contains(value, ": ") || strings.Contains(value, " #") ||
		strings.HasSuffix(value, ":") ||
		strings.ContainsAny(value[:1], "&*!|>'\"%@`[]{},?-#:") ||
		value != strings.TrimSpace(value)
	// Anything else plain YAML resolves to a number, bool, null, or date
	// ("2026", "0x1F", "y", "2026-09-23") stays a string by being quoted.
	if !needsQuote {
		var decoded map[string]interface{}
		err := yaml.Unmarshal([]byte("v: "+value), &decoded)
		needsQuote = err != nil || decoded["v"] != value || yamlDatePrefix.MatchString(value)
	}
	if !needsQuote {
		return value
	}
	// A JSON string is a valid YAML double-quoted scalar. HTML escaping stays
	// off so "R&D" does not come out as "R\u0026D".
	var quoted bytes.Buffer
	encoder := json.NewEncoder(&quoted)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	return strings.TrimSuffix(quoted.String(), "\n")
}

// Accessors for decoded JSON.

func asMap(value interface{}) map[string]interface{} {
	m, _ := value.(map[string]interface{})
	return m
}

func asList(value interface{}) []interface{} {
	l, _ := value.([]interface{})
	return l
}

// scalar formats a decoded JSON scalar for display: integral numbers without
// a decimal point, nothing for null.
func scalar(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return fmt.Sprint(value)
}

func isScalar(value interface{}) bool {
	switch value.(type) {
	case nil, string, bool, float64:
		return true
	}
	return false
}
