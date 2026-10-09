package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// sessionRows is one page of a session in the shapes the API returns: a
// person's Slack turn, the agent's thinking, a tool call and its result, the
// agent's reply, and the marker rows around them.
const sessionRows = `{
  "data": [
    {"id": 1, "type": "system", "status": "sent", "channel": "slack", "created_at": "2026-10-09T13:00:00.000000Z",
     "actor": {"kind": "ai", "display_name": "Jin Wallfacer"},
     "payload": {"type": "system", "subtype": "init"}},
    {"id": 2, "type": "user", "status": "sent", "channel": "slack", "created_at": "2026-10-09T13:00:01.500000Z",
     "actor": {"kind": "external", "provider": "slack", "display_name": "Paul Denya"},
     "display_text": null,
     "payload": {"type": "user", "message": {"role": "user", "content": [{"type": "text", "text": "How do you read past sessions?"}]}}},
    {"id": 3, "type": "assistant", "status": "sent", "channel": "slack", "created_at": "2026-10-09T13:00:02Z",
     "actor": {"kind": "ai", "display_name": "Jin Wallfacer"},
     "payload": {"type": "assistant", "message": {"role": "assistant", "content": [{"type": "thinking", "thinking": "", "signature": "abc"}]}}},
    {"id": 4, "type": "assistant", "status": "sent", "channel": "slack", "created_at": "2026-10-09T13:00:03Z",
     "actor": {"kind": "ai", "display_name": "Jin Wallfacer"},
     "payload": {"type": "assistant", "message": {"role": "assistant", "content": [{"type": "tool_use", "id": "toolu_1", "name": "mcp__sandbox__bash", "input": {"command": "ls"}}]}}},
    {"id": 5, "type": "user", "status": "sent", "channel": "slack", "created_at": "2026-10-09T13:00:04Z",
     "actor": {"kind": "system", "subsystem": "platform"},
     "payload": {"type": "user", "message": {"role": "user", "content": [{"type": "tool_result", "tool_use_id": "toolu_1", "content": [{"type": "text", "text": "exit_code=0\nREADME.md"}]}]}}},
    {"id": 6, "type": "assistant", "status": "sent", "channel": "slack", "created_at": "2026-10-09T13:00:05Z",
     "actor": {"kind": "ai", "display_name": "Jin Wallfacer"},
     "payload": {"type": "assistant", "message": {"role": "assistant", "content": [{"type": "text", "text": "With the messages list command."}]}}},
    {"id": 7, "type": "result", "status": "sent", "channel": "slack", "created_at": "2026-10-09T13:00:06Z",
     "actor": {"kind": "ai", "display_name": "Jin Wallfacer"},
     "payload": {"type": "result", "subtype": "success", "is_error": false, "num_turns": 3, "duration_ms": 4210}}
  ],
  "meta": {"next_cursor": "abc", "per_page": 7}
}`

const singleRow = `{"data": {"id": 4, "type": "assistant", "status": "sent", "channel": "app", "created_at": "2026-10-09T13:00:03Z",
  "actor": {"kind": "ai", "display_name": "Jin Wallfacer"},
  "payload": {"type": "assistant", "message": {"role": "assistant", "content": [{"type": "tool_use", "id": "toolu_1", "name": "Read", "input": {"path": "/tmp/x"}}]}}}}`

// messagesTree is the generated `messages` group the way the real CLI has it
// after registration: list and get marked generated and given their text
// views, next to a generated command with no text view. The generated Run
// bodies only record that they ran.
type messagesTree struct {
	root      *cobra.Command
	generated []string
	requests  []string
}

func newMessagesTree(t *testing.T, accountID string) *messagesTree {
	t.Helper()
	tree := &messagesTree{root: &cobra.Command{Use: "wallfacer"}}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tree.requests = append(tree.requests, r.URL.Path+"?"+r.URL.RawQuery)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/messages") {
			_, _ = w.Write([]byte(sessionRows))
			return
		}
		_, _ = w.Write([]byte(singleRow))
	}))
	t.Cleanup(server.Close)
	viper.Set("server", server.URL)
	t.Cleanup(func() { viper.Set("server", "") })

	generated := func(name string) func(*cobra.Command, []string) {
		return func(*cobra.Command, []string) { tree.generated = append(tree.generated, name) }
	}
	registerGenerated(tree.root, func() {
		messages := &cobra.Command{Use: "messages"}
		list := &cobra.Command{Use: "list task-id session-id", Run: generated("messages list")}
		list.Flags().String("view", "", "")
		list.Flags().String("direction", "", "")
		list.Flags().String("cursor", "", "")
		list.Flags().Int64("per-page", 0, "")
		get := &cobra.Command{Use: "get task-id session-id message-id", Run: generated("messages get")}
		messages.AddCommand(list, get)

		tasks := &cobra.Command{Use: "tasks"}
		tasks.AddCommand(&cobra.Command{Use: "list", Run: generated("tasks list")})

		tree.root.AddCommand(messages, tasks)
	})
	registerSessionTextViews(tree.root, accountID)
	return tree
}

// run executes the tree with the given -o value and returns what it printed.
func (tree *messagesTree) run(t *testing.T, format string, args ...string) string {
	t.Helper()
	viper.Set("output-format", format)
	viper.Set("query", "")
	defer viper.Set("output-format", "")

	var buf bytes.Buffer
	previous := cli.Stdout
	cli.Stdout = &buf
	defer func() { cli.Stdout = previous }()

	tree.root.SetArgs(args)
	if err := tree.root.Execute(); err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return buf.String()
}

func TestRegisterGeneratedMarksOnlyTheCommandsItAdded(t *testing.T) {
	tree := newMessagesTree(t, "acct")
	tree.root.AddCommand(&cobra.Command{Use: "handbook", Run: func(*cobra.Command, []string) {}})

	for path, want := range map[string]bool{
		"messages list": true,
		"tasks list":    true,
		"handbook":      false,
	} {
		cmd, _, err := tree.root.Find(strings.Fields(path))
		if err != nil {
			t.Fatal(err)
		}
		if got := cmd.Annotations[annotationGenerated] != ""; got != want {
			t.Errorf("%s: generated = %v, want %v", path, got, want)
		}
	}
}

func TestTextIsRefusedOnGeneratedCommandsWithoutATextView(t *testing.T) {
	tree := newMessagesTree(t, "acct")
	defer viper.Set("output-format", "")
	defer viper.Set("query", "")

	for _, tc := range []struct {
		path, format, query string
		refused             bool
	}{
		{"tasks list", "text", "", true},
		{"tasks list", "TEXT", "", true},
		{"tasks list", "", "", false},
		{"tasks list", "json", "", false},
		{"tasks list", "yaml", "", false},
		{"tasks list", "text", "data[0].id", false},
		{"messages list", "text", "", false},
		{"messages get", "text", "", false},
	} {
		cmd, _, err := tree.root.Find(strings.Fields(tc.path))
		if err != nil {
			t.Fatal(err)
		}
		viper.Set("output-format", tc.format)
		viper.Set("query", tc.query)
		err = requireTextView(cmd, nil)
		if (err != nil) != tc.refused {
			t.Errorf("%s -o %q -q %q: error = %v, refused want %v", tc.path, tc.format, tc.query, err, tc.refused)
		}
		if err != nil && !strings.Contains(err.Error(), "wallfacer tasks list has no text output") {
			t.Errorf("the refusal should name the command and the formats it has: %v", err)
		}
	}

	// A hand-written command decides its own formats.
	viper.Set("output-format", "text")
	if err := requireTextView(&cobra.Command{Use: "handbook"}, nil); err != nil {
		t.Errorf("a command that is not generated should not be refused: %v", err)
	}
}

func TestMessagesListDefaultsToTheGeneratedJSON(t *testing.T) {
	tree := newMessagesTree(t, "acct")

	for _, format := range []string{"", "json", "yaml"} {
		tree.generated = nil
		tree.run(t, format, "messages", "list", "task", "sess")
		if len(tree.generated) != 1 || len(tree.requests) != 0 {
			t.Errorf("-o %q should run the generated command untouched: ran %v, text view requested %v", format, tree.generated, tree.requests)
		}
	}
}

func TestMessagesListTextIsAnAttributedConversation(t *testing.T) {
	tree := newMessagesTree(t, "acct")
	out := tree.run(t, "text", "messages", "list", "task", "sess", "--per-page", "7")

	if len(tree.generated) != 0 {
		t.Errorf("-o text should not fall through to the JSON command: %v", tree.generated)
	}
	if len(tree.requests) != 1 || tree.requests[0] != "/v1/accounts/acct/tasks/task/sessions/sess/messages?per_page=7&view=trimmed" {
		t.Errorf("text view should fetch the trimmed page in the configured account: %v", tree.requests)
	}

	want := `### Paul Denya · user · slack · 2026-10-09 13:00:01Z · #2
How do you read past sessions?

### Jin Wallfacer · assistant · 2026-10-09 13:00:05Z · #6
With the messages list command.

Not shown: 2 rows of tool calls and results (--tools), 2 marker rows (--markers).

Next:
  next page  wallfacer messages list task sess --per-page 7 -o text --cursor abc
`
	if out != want {
		t.Errorf("text view:\n%s\nwant:\n%s", out, want)
	}
}

func TestMessagesListTextShowsToolsAndMarkersOnRequest(t *testing.T) {
	tree := newMessagesTree(t, "acct")
	out := tree.run(t, "text", "messages", "list", "task", "sess", "--tools", "--markers", "--view", "full")

	if len(tree.requests) != 1 || !strings.Contains(tree.requests[0], "view=full") {
		t.Errorf("an explicit --view should be kept: %v", tree.requests)
	}
	assertContains(t, out,
		"### marker: system init · 2026-10-09 13:00:00Z · #1\n",
		"### Jin Wallfacer · tool call · 2026-10-09 13:00:03Z · #4\nmcp__sandbox__bash {\"command\":\"ls\"}\n",
		"### tool result: mcp__sandbox__bash · 2026-10-09 13:00:04Z · #5\nexit_code=0\nREADME.md\n",
		"### marker: result success · 3 turns · 4.2s · 2026-10-09 13:00:06Z · #7\n",
		"wallfacer messages list task sess --markers --tools --view full -o text --cursor abc",
	)
	if strings.Contains(out, "Not shown") {
		t.Errorf("nothing was hidden, so nothing should be counted:\n%s", out)
	}
	if strings.Index(out, "#2") > strings.Index(out, "#4") || strings.Index(out, "#4") > strings.Index(out, "#6") {
		t.Errorf("rows should keep the order the API returned them in:\n%s", out)
	}
}

func TestMessagesListTextWithoutAConfiguredAccount(t *testing.T) {
	tree := newMessagesTree(t, "")
	out := tree.run(t, "text", "messages", "list", "acct2", "task", "sess")

	if len(tree.requests) != 1 || !strings.HasPrefix(tree.requests[0], "/v1/accounts/acct2/tasks/task/sessions/sess/messages?") {
		t.Errorf("the account ID should come from the arguments: %v", tree.requests)
	}
	assertContains(t, out, "wallfacer messages list acct2 task sess -o text --cursor abc")
}

func TestMessagesGetTextShowsTheRowWhateverItIs(t *testing.T) {
	tree := newMessagesTree(t, "acct")
	out := tree.run(t, "text", "messages", "get", "task", "sess", "4")

	if len(tree.requests) != 1 || !strings.HasPrefix(tree.requests[0], "/v1/accounts/acct/tasks/task/sessions/sess/messages/4?") {
		t.Errorf("requests: %v", tree.requests)
	}
	want := "### Jin Wallfacer · tool call · 2026-10-09 13:00:03Z · #4\nRead {\"path\":\"/tmp/x\"}\n"
	if out != want {
		t.Errorf("text view:\n%s\nwant:\n%s", out, want)
	}
}

func TestTextOnlyFlagsNeedTheTextView(t *testing.T) {
	tree := newMessagesTree(t, "acct")
	list, _, _ := tree.root.Find([]string{"messages", "list"})
	defer viper.Set("output-format", "")

	if err := list.Flags().Set("tools", "true"); err != nil {
		t.Fatal(err)
	}
	viper.Set("output-format", "json")
	if err := textOnlyFlags(list); err == nil || !strings.Contains(err.Error(), "--tools shapes the text view") {
		t.Errorf("--tools with JSON output should be refused, got %v", err)
	}
	viper.Set("output-format", "text")
	if err := textOnlyFlags(list); err != nil {
		t.Errorf("--tools with -o text: %v", err)
	}
}

func TestConversationViewDetails(t *testing.T) {
	view := &conversationView{tools: true}
	t.Run("display text replaces a user turn's prepended context", func(t *testing.T) {
		out := &textOut{}
		view.render(out, map[string]interface{}{
			"id": float64(9), "type": "user", "status": "queued", "channel": "app",
			"author":       map[string]interface{}{"name": "Héctor Ramos"},
			"actor":        map[string]interface{}{"kind": "user"},
			"display_text": "Ship it",
			"payload": map[string]interface{}{"message": map[string]interface{}{
				"content": []interface{}{map[string]interface{}{"type": "text", "text": "<attachments>...</attachments>\nShip it"}},
			}},
		})
		if got, want := out.String(), "### Héctor Ramos · user · app · queued · #9\nShip it\n"; got != want {
			t.Errorf("got:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("an elided tool input names its size and where to get it", func(t *testing.T) {
		got := toolPayload(map[string]interface{}{"elided": true, "byte_size": float64(81234)})
		if got != "[elided: 81234 bytes; `messages get` returns it whole]" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("a failed tool result says so", func(t *testing.T) {
		out := &textOut{}
		view.render(out, map[string]interface{}{
			"id": float64(10), "type": "user", "actor": map[string]interface{}{"kind": "system"},
			"payload": map[string]interface{}{"message": map[string]interface{}{
				"content": []interface{}{map[string]interface{}{"type": "tool_result", "tool_use_id": "unknown", "is_error": true, "content": "boom"}},
			}},
		})
		if got, want := out.String(), "### tool error · #10\nboom\n"; got != want {
			t.Errorf("got:\n%s\nwant:\n%s", got, want)
		}
	})
}
