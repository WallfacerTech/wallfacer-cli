package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// The root help is the first thing a person or an agent sees, often with no
// other context about Wallfacer. It says what Wallfacer is, names the handful
// of commands that cover most work, defines the words the rest of the help
// uses, and groups the commands by job. The generated API groups, one per API
// resource, are listed by name at the end rather than as forty "Manage X"
// lines, since the curated commands above cover the same ground in fewer
// steps.

const rootIntro = `Wallfacer gives a team AI employees: agents with a role, their own computer,
and a handbook to work from, doing real work under human approval. This CLI acts
on the same account as the Wallfacer app.`

var rootStartHere = [][2]string{
	{"wallfacer auth status", "Check you are signed in and list your accounts"},
	{"wallfacer team list", "The agents and people on the account"},
	{"wallfacer handbook tree", "Everything the team has written down: pages and playbooks"},
	{"wallfacer handbook read <name|path|id>", "Read one page or playbook"},
	{"wallfacer chat <agent> \"<request>\"", "Hand an agent work in plain language"},
	{"wallfacer run <playbook>", "Start a task from a published playbook"},
	{"wallfacer tasks list --needs-me true", "Tasks waiting on you"},
}

var rootGlossary = [][2]string{
	{"agent", "An AI employee: a name, a title, a role page, its own computer and model."},
	{"handbook", "The account's tree of pages (knowledge) and playbooks (workflows)."},
	{"playbook", "Ordered AI, human, and wait steps plus the triggers that start them. The API calls it a pipeline."},
	{"task", "One piece of work: a conversation with an agent, or one run of a playbook."},
	{"computer", "The machine definition an agent works on. The API calls it an environment."},
}

type helpGroup struct {
	title    string
	commands []string
}

var rootGroups = []helpGroup{
	{"Work", []string{"chat", "run", "tasks", "sessions", "messages", "events"}},
	{"Team and handbook", []string{"team", "handbook", "agents"}},
	{"Computers", []string{"environments", "snapshots", "secrets", "vms", "up", "exec"}},
	{"Account", []string{"auth", "accounts", "users", "invitations", "credentials", "keys", "collectors"}},
}

// rootShorts replace the generated "Manage X" descriptions of the commands the
// root help lists by name. Each says what the resource is, in product terms.
var rootShorts = map[string]string{
	"auth":         "Sign in, sign out, and check which accounts you can reach",
	"tasks":        "List, inspect, direct, and retry tasks",
	"sessions":     "The agent sessions inside a task, and their logs",
	"messages":     "Read and send the messages in a session",
	"events":       "Events that start and update tasks; ingest your own",
	"agents":       "Hire, configure, and disable agents",
	"environments": "Computers: the machine definitions agents work on",
	"snapshots":    "A computer's built images: list, regenerate, roll back",
	"secrets":      "Encrypted secrets a computer's definition references",
	"vms":          "Running VMs: create, inspect, run commands, destroy",
	"accounts":     "Account details, analytics, and audit logs",
	"users":        "The account's human members and their roles",
	"invitations":  "Invite people to the account",
	"credentials":  "Vendor API keys and subscription seats agents run on",
	"keys":         "Event keys that let other systems post events",
	"collectors":   "Webhook URLs that turn other systems' webhooks into events",
}

// hiddenFromRoot are commands about the CLI itself, reached through the help
// footer instead of the command list.
var hiddenFromRoot = map[string]bool{"help": true, "help-config": true, "help-input": true}

// configureRootHelp installs the root's text and help screen. Subcommands keep
// cobra's standard help.
func configureRootHelp(root *cobra.Command) {
	root.Short = "Wallfacer: AI employees with a role, a computer, and a handbook"
	root.Long = rootIntro
	for _, cmd := range root.Commands() {
		if short, ok := rootShorts[cmd.Name()]; ok {
			cmd.Short = short
		}
	}

	standard := root.HelpFunc()
	root.SetHelpFunc(func(c *cobra.Command, args []string) {
		if c != root {
			standard(c, args)
			return
		}
		writeRootHelp(c.OutOrStdout(), root)
	})
}

func writeRootHelp(w io.Writer, root *cobra.Command) {
	var b strings.Builder

	b.WriteString(rootIntro + "\n\n")

	b.WriteString("Start here:\n")
	writeColumns(&b, rootStartHere)
	b.WriteString("\n")

	b.WriteString("Words used here:\n")
	writeColumns(&b, rootGlossary)
	b.WriteString("\n")

	b.WriteString("Usage:\n  wallfacer <command> [flags]\n\n")

	byName := map[string]*cobra.Command{}
	for _, cmd := range root.Commands() {
		if cmd.IsAvailableCommand() {
			byName[cmd.Name()] = cmd
		}
	}
	listed := map[string]bool{}
	for _, group := range rootGroups {
		var rows [][2]string
		for _, name := range group.commands {
			if cmd, ok := byName[name]; ok {
				rows = append(rows, [2]string{name, cmd.Short})
				listed[name] = true
			}
		}
		if len(rows) == 0 {
			continue
		}
		b.WriteString(group.title + ":\n")
		writeColumns(&b, rows)
		b.WriteString("\n")
	}

	var rest []string
	for name := range byName {
		if !listed[name] && !hiddenFromRoot[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	if len(rest) > 0 {
		b.WriteString("API resources (one command per API resource, for scripting; they print JSON):\n")
		b.WriteString(wrapWords(strings.Join(rest, ", "), 78, "  "))
		b.WriteString("\n\n")
	}

	b.WriteString("Flags:\n")
	b.WriteString(root.NonInheritedFlags().FlagUsages())
	b.WriteString("\n")
	b.WriteString(`Every command takes --help. Handbook commands print text by default; add
-o json for scripting. "wallfacer help-config" covers configuration and
environment variables, "wallfacer help-input" covers request bodies.

Docs: https://wallfacer.ai/docs
`)

	fmt.Fprint(w, b.String())
}

func writeColumns(b *strings.Builder, rows [][2]string) {
	width := 0
	for _, row := range rows {
		if len(row[0]) > width {
			width = len(row[0])
		}
	}
	for _, row := range rows {
		fmt.Fprintf(b, "  %-*s  %s\n", width, row[0], row[1])
	}
}

func wrapWords(text string, width int, indent string) string {
	var lines []string
	line := indent
	for _, word := range strings.Fields(text) {
		if len(line) > len(indent) && len(line)+1+len(word) > width {
			lines = append(lines, line)
			line = indent
		}
		if len(line) > len(indent) {
			line += " "
		}
		line += word
	}
	if len(line) > len(indent) {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
