package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Generated API commands print JSON or YAML. A few of them also have a text
// view, registered here; the rest refuse an explicit -o text rather than
// printing JSON to a caller who asked for something else.
const (
	annotationGenerated = "wallfacer/generated"
	annotationTextView  = "wallfacer/text-view"
)

// registerGenerated runs register and marks every runnable command it added as
// generated, so the output-format check can tell the API commands from the
// hand-written ones registered around them.
func registerGenerated(root *cobra.Command, register func()) {
	before := map[*cobra.Command]bool{}
	walkCommands(root, func(cmd *cobra.Command) { before[cmd] = true })

	register()

	walkCommands(root, func(cmd *cobra.Command) {
		if before[cmd] || !cmd.Runnable() {
			return
		}
		if cmd.Annotations == nil {
			cmd.Annotations = map[string]string{}
		}
		cmd.Annotations[annotationGenerated] = "true"
	})
}

func walkCommands(cmd *cobra.Command, visit func(*cobra.Command)) {
	visit(cmd)
	for _, sub := range cmd.Commands() {
		walkCommands(sub, visit)
	}
}

// textRequested reports whether the caller explicitly asked for text. A
// generated command prints JSON when nothing is asked for, and --query always
// projects structured data, so neither of those counts.
func textRequested() bool {
	format := strings.ToLower(strings.TrimSpace(viper.GetString("output-format")))
	return format == formatText && viper.GetString("query") == ""
}

// requireTextView refuses -o text on a generated command with no text view.
// The generated formatter would print JSON for it, and a caller who asked for
// text would get JSON without being told.
func requireTextView(cmd *cobra.Command, args []string) error {
	if cmd.Annotations[annotationGenerated] == "" || cmd.Annotations[annotationTextView] != "" {
		return nil
	}
	if !textRequested() {
		return nil
	}
	return errors.Errorf("%s has no text output: use -o json or -o yaml", cmd.CommandPath())
}

// registerSessionTextViews gives `messages list` and `messages get` a text
// view: the session as a conversation, attributed and in order. JSON stays the
// default, unchanged, for scripts. It runs after injectAccountID, so args are
// what the caller typed and the account ID is added back here when it was
// configured.
func registerSessionTextViews(root *cobra.Command, accountID string) {
	if list := findSubcommand(root, "messages", "list"); list != nil {
		list.Long += "\n\n" + cli.Markdown("With `-o text`, prints the page as a conversation: each person's and agent's turns, attributed and in order, fetched with `view=trimmed` unless `--view` says otherwise. Tool calls and results are left out unless `--tools` is passed, and the transcript's marker rows (session start and end, results, titles) unless `--markers` is. A line at the end counts what was left out, and `Next:` gives the command for the following page.")
		list.Flags().Bool("tools", false, "With -o text, include tool calls and their results")
		list.Flags().Bool("markers", false, "With -o text, include marker rows (session start and end, results, titles)")
		addTextView(list, accountID, 3, runMessagesListText)
	}
	if get := findSubcommand(root, "messages", "get"); get != nil {
		get.Long += "\n\n" + cli.Markdown("With `-o text`, prints the message the way `messages list -o text --tools --markers` would, whatever kind of row it is.")
		addTextView(get, accountID, 4, runMessagesGetText)
	}
}

func findSubcommand(root *cobra.Command, path ...string) *cobra.Command {
	cmd, rest, err := root.Find(path)
	if err != nil || len(rest) > 0 || cmd == root {
		return nil
	}
	return cmd
}

// addTextView routes -o text to render and everything else to the generated
// command. ids is how many positional IDs the API call takes, the account ID
// included.
func addTextView(cmd *cobra.Command, accountID string, ids int, render func(cmd *cobra.Command, ids, args []string) error) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[annotationTextView] = "true"
	generated := cmd.Run
	cmd.Run = func(c *cobra.Command, args []string) {
		err := textOnlyFlags(c)
		if err == nil && !textRequested() {
			generated(c, args)
			return
		}
		if err == nil {
			full := args
			if accountID != "" {
				full = append([]string{accountID}, args...)
			}
			if len(full) != ids {
				err = errors.Errorf("usage: %s", c.UseLine())
			} else {
				err = render(c, full, args)
			}
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}

// textOnlyFlags refuses --tools and --markers outside the text view, where
// they would otherwise be ignored: the JSON always carries every row.
func textOnlyFlags(cmd *cobra.Command) error {
	if textRequested() {
		return nil
	}
	for _, name := range []string{"tools", "markers"} {
		if flag := cmd.Flags().Lookup(name); flag != nil && flag.Changed {
			return errors.Errorf("--%s shapes the text view: add -o text, or drop it to get every row as JSON", name)
		}
	}
	return nil
}

func runMessagesListText(cmd *cobra.Command, ids, args []string) error {
	params := viper.New()
	if err := params.BindPFlags(cmd.Flags()); err != nil {
		return err
	}
	if !cmd.Flags().Changed("view") {
		params.Set("view", "trimmed")
	}
	_, decoded, err := OpenapiListMessages(ids[0], ids[1], ids[2], params)
	if err != nil {
		return err
	}

	tools, _ := cmd.Flags().GetBool("tools")
	markers, _ := cmd.Flags().GetBool("markers")
	view := &conversationView{tools: tools, markers: markers}
	t := &textOut{}
	rows := asList(decoded["data"])
	for _, row := range rows {
		view.render(t, asMap(row))
	}
	if len(rows) == 0 {
		t.line("No messages.")
	}
	view.summary(t)

	if cursor := scalar(asMap(decoded["meta"])["next_cursor"]); cursor != "" {
		t.next(map[string]interface{}{"next_page": messagesNextPage(cmd, args, cursor)}, nil)
	}
	_, err = io.WriteString(cli.Stdout, t.String())
	return err
}

func runMessagesGetText(cmd *cobra.Command, ids, args []string) error {
	params := viper.New()
	if err := params.BindPFlags(cmd.Flags()); err != nil {
		return err
	}
	_, decoded, err := OpenapiGetAMessage(ids[0], ids[1], ids[2], ids[3], params)
	if err != nil {
		return err
	}

	view := &conversationView{tools: true, markers: true}
	t := &textOut{}
	row := asMap(decoded["data"])
	if row == nil {
		row = decoded
	}
	view.render(t, row)
	_, err = io.WriteString(cli.Stdout, t.String())
	return err
}

// messagesNextPage repeats the command as typed, with the cursor for the next
// page in place of any cursor it was given.
func messagesNextPage(cmd *cobra.Command, args []string, cursor string) string {
	parts := []string{cmd.CommandPath()}
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	cmd.Flags().Visit(func(flag *pflag.Flag) {
		switch flag.Name {
		case "cursor", "output-format":
			return
		}
		if flag.Value.Type() == "bool" {
			if flag.Value.String() == "true" {
				parts = append(parts, "--"+flag.Name)
			}
			return
		}
		parts = append(parts, "--"+flag.Name, shellQuote(flag.Value.String()))
	})
	parts = append(parts, "-o", formatText, "--cursor", shellQuote(cursor))
	return strings.Join(parts, " ")
}

// conversationView renders transcript rows as a conversation. It keeps the
// names of the tool calls it has seen, so a result can say which tool it
// answers, and counts the rows it left out.
type conversationView struct {
	tools, markers bool

	toolNames     map[string]string
	hiddenTools   int
	hiddenMarkers int
}

// render writes one row. A `user` or `assistant` row is a turn when it has
// text to show; its tool calls and results are tool detail; every other type
// is a marker. Thinking blocks are never shown: the JSON carries them.
func (v *conversationView) render(t *textOut, row map[string]interface{}) {
	payload := asMap(row["payload"])
	kind := scalar(row["type"])
	if kind != "user" && kind != "assistant" {
		if !v.markers {
			v.hiddenMarkers++
			return
		}
		t.gap()
		t.text(header(marker(kind, payload), row))
		return
	}

	var text []string
	var tools []map[string]interface{}
	switch content := asMap(payload["message"])["content"].(type) {
	case string:
		text = append(text, content)
	case []interface{}:
		for _, item := range content {
			block := asMap(item)
			switch scalar(block["type"]) {
			case "text":
				text = append(text, scalar(block["text"]))
			case "image":
				text = append(text, "[image]")
			case "tool_use", "tool_result":
				tools = append(tools, block)
			}
		}
	}
	if display := scalar(row["display_text"]); kind == "user" && display != "" {
		text = []string{display}
	}

	if body := strings.TrimSpace(strings.Join(text, "\n\n")); body != "" {
		labels := []string{speaker(row), kind}
		if kind == "user" {
			labels = append(labels, scalar(row["channel"]))
		}
		t.gap()
		t.text(header(labels, row))
		t.block(body)
	}

	if len(tools) == 0 {
		return
	}
	if !v.tools {
		v.hiddenTools++
		return
	}
	for _, block := range tools {
		t.gap()
		v.tool(t, row, block)
	}
}

// tool writes one tool call or tool result.
func (v *conversationView) tool(t *textOut, row, block map[string]interface{}) {
	id := scalar(block["id"])
	if scalar(block["type"]) == "tool_use" {
		name := scalar(block["name"])
		if v.toolNames == nil {
			v.toolNames = map[string]string{}
		}
		v.toolNames[id] = name
		t.text(header([]string{speaker(row), "tool call"}, row))
		t.block(name + " " + toolPayload(block["input"]))
		return
	}

	label := "tool result"
	if block["is_error"] == true {
		label = "tool error"
	}
	if name := v.toolNames[scalar(block["tool_use_id"])]; name != "" {
		label += ": " + name
	}
	t.text(header([]string{label}, row))
	t.block(toolPayload(block["content"]))
}

// summary says what the view left out and how to show it.
func (v *conversationView) summary(t *textOut) {
	var hidden []string
	if v.hiddenTools > 0 {
		hidden = append(hidden, plural(v.hiddenTools, "row", "rows")+" of tool calls and results (--tools)")
	}
	if v.hiddenMarkers > 0 {
		hidden = append(hidden, plural(v.hiddenMarkers, "marker row", "marker rows")+" (--markers)")
	}
	if len(hidden) == 0 {
		return
	}
	t.gap()
	t.line("Not shown: %s.", strings.Join(hidden, ", "))
}

// header is the line above each entry: who or what, then the row's state when
// it is not an ordinary delivered message, the time, and the message ID that
// `messages get` takes.
func header(labels []string, row map[string]interface{}) string {
	if status := scalar(row["status"]); status != "" && status != "sent" {
		labels = append(labels, status)
	}
	if mode := scalar(row["mode"]); mode != "" {
		labels = append(labels, mode)
	}
	labels = append(labels, timestamp(scalar(row["created_at"])), "#"+scalar(row["id"]))

	shown := labels[:0]
	for _, label := range labels {
		if label != "" && label != "#" {
			shown = append(shown, label)
		}
	}
	return "### " + strings.Join(shown, " · ")
}

// speaker names who produced a row, from the actor the API captured when it
// was written. The platform's own rows are named as such.
func speaker(row map[string]interface{}) string {
	actor := asMap(row["actor"])
	if name := scalar(actor["display_name"]); name != "" {
		return name
	}
	if name := scalar(asMap(row["author"])["name"]); name != "" {
		return name
	}
	if scalar(actor["kind"]) == "system" {
		return "Platform"
	}
	return scalar(row["type"])
}

// marker labels a non-conversational row: its type, plus the one or two fields
// that say what happened.
func marker(kind string, payload map[string]interface{}) []string {
	label := "marker: " + kind
	if subtype := scalar(payload["subtype"]); subtype != "" {
		label += " " + subtype
	}
	labels := []string{label}

	switch kind {
	case "ai-title":
		labels = append(labels, yamlScalar(oneLine(scalar(payload["aiTitle"]))))
	case "queue-operation":
		labels = append(labels, scalar(payload["operation"]))
	case "result":
		if payload["is_error"] == true {
			labels = append(labels, "error")
		}
		if turns := scalar(payload["num_turns"]); turns != "" {
			labels = append(labels, turns+" turns")
		}
		if ms, ok := payload["duration_ms"].(float64); ok {
			labels = append(labels, (time.Duration(ms) * time.Millisecond).Round(100*time.Millisecond).String())
		}
	}
	return labels
}

// toolPayload renders a tool input or result. Text stays as text; anything
// else is compact JSON. A block the trimmed view elided is named with its size
// and the command that returns it whole.
func toolPayload(value interface{}) string {
	if m := asMap(value); m["elided"] == true {
		return fmt.Sprintf("[elided: %s bytes; `messages get` returns it whole]", scalar(m["byte_size"]))
	}
	switch v := value.(type) {
	case string:
		return v
	case []interface{}:
		var parts []string
		for _, item := range v {
			block := asMap(item)
			switch kind := scalar(block["type"]); kind {
			case "text":
				parts = append(parts, scalar(block["text"]))
			case "":
				parts = append(parts, compactJSON(item))
			default:
				parts = append(parts, "["+kind+"]")
			}
		}
		return strings.Join(parts, "\n")
	}
	return compactJSON(value)
}

func compactJSON(value interface{}) string {
	var b strings.Builder
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return fmt.Sprint(value)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// timestamp shortens an API timestamp to the second, in UTC.
func timestamp(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	return parsed.UTC().Format("2006-01-02 15:04:05Z")
}
