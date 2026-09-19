package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// Chat and run are two different things to ask for, so they are two commands.
// Chat sends the freeform `prompt` contract to an agent you name; run starts
// the account's published playbook through `pipeline_id`. Neither ever builds
// the other's body, and the task endpoint does not accept both fields at once.

func registerChatCommand(accountID string) {
	cmd := &cobra.Command{
		Use:   "chat <agent> [prompt]",
		Short: "Start a conversation with one of the account's agents",
		Long: cli.Markdown(`Creates a task addressed to the agent you name, with the prompt as its first message.

The agent reference is a member ID, a unique display name, an agent handle (with or
without a leading ` + "`@`" + `), or an email address; resolve one first with
` + "`wallfacer team get <reference>`" + `. A human member, and an agent that is disabled or
paused, are refused by name rather than quietly replaced with another identity.

The prompt is read from stdin when it is not given as an argument. This is the freeform
contract: the request carries ` + "`prompt`" + ` and never ` + "`pipeline_id`" + `. To run a
published playbook instead, use ` + "`wallfacer run`" + `.`),
		Args: cobra.RangeArgs(1, 2),
		Run: teamRun(accountID, func(api *directoryAPI, cmd *cobra.Command, args []string) error {
			prompt := ""
			if len(args) == 2 {
				prompt = args[1]
			} else {
				piped, err := promptFromStdin()
				if err != nil {
					return err
				}
				prompt = piped
			}

			opts := taskOptions{}
			opts.title, _ = cmd.Flags().GetString("title")
			opts.environmentID, _ = cmd.Flags().GetString("environment-id")
			opts.harness, _ = cmd.Flags().GetString("harness")
			opts.model, _ = cmd.Flags().GetString("model")
			opts.effort, _ = cmd.Flags().GetString("effort")
			opts.idleTimeout, _ = cmd.Flags().GetInt("idle-timeout")

			return runChat(api, args[0], prompt, opts)
		}),
	}

	cmd.Flags().String("title", "", "Short title for the task (generated from the prompt when omitted)")
	cmd.Flags().String("environment-id", "", "Environment for the task's sessions (defaults to the agent's own)")
	cmd.Flags().String("harness", "", "Agent harness to run (e.g. claude, codex)")
	cmd.Flags().String("model", "", "Model to pin on the session, scoped to the harness")
	cmd.Flags().String("effort", "", "Reasoning effort to pin on the session, in the harness's own vocabulary")
	cmd.Flags().Int("idle-timeout", 0, "Seconds of inactivity before the task's sessions go idle (60-900)")

	cli.Root.AddCommand(cmd)
}

func registerRunCommand(accountID string) {
	cmd := &cobra.Command{
		Use:   "run <playbook>",
		Short: "Run one of the account's published playbooks",
		Long: cli.Markdown(`Starts a task against a published playbook, on the version the server has active.

The playbook reference is anything ` + "`wallfacer handbook`" + ` accepts for a playbook: a
stable ID, a unique name, a full path through the tree, or a playbook detail URL inside
the configured account. A page is not a playbook and is refused as one, a reference from
another account is refused before any request goes out, and a playbook with nothing
published cannot be run: publish the draft first. A disabled playbook does run: its
triggers are cleared, so a manual run is the deliberate way to fire one.

The run follows the steps of the active published version, and each step runs as the
actor that version names. ` + "`--agent`" + ` sets the task's own identity and default
environment; it does not reassign the playbook's step actors. To talk to an agent
freeform instead, use ` + "`wallfacer chat`" + `.`),
		Args: cobra.ExactArgs(1),
		Run: teamRun(accountID, func(api *directoryAPI, cmd *cobra.Command, args []string) error {
			message, _ := cmd.Flags().GetString("message")
			agent, _ := cmd.Flags().GetString("agent")

			opts := taskOptions{}
			opts.title, _ = cmd.Flags().GetString("title")
			opts.environmentID, _ = cmd.Flags().GetString("environment-id")
			opts.idleTimeout, _ = cmd.Flags().GetInt("idle-timeout")

			return runPlaybook(api, args[0], message, agent, opts)
		}),
	}

	cmd.Flags().String("message", "", "Kickoff instruction recorded as the run's triggering context")
	cmd.Flags().String("agent", "", "Agent the task runs as: its task identity and default environment, not an override of the playbook's step actors")
	cmd.Flags().String("title", "", "Short title for the task")
	cmd.Flags().String("environment-id", "", "Default environment for the task's sessions")
	cmd.Flags().Int("idle-timeout", 0, "Seconds of inactivity before the task's sessions go idle (60-900)")

	cli.Root.AddCommand(cmd)
}

// taskOptions are the task-create fields both entry points share. What they do
// not share is the field that decides the execution mode.
type taskOptions struct {
	title         string
	environmentID string
	harness       string
	model         string
	effort        string
	idleTimeout   int
}

func applyTaskOptions(body map[string]interface{}, opts taskOptions) {
	if opts.title != "" {
		body["title"] = opts.title
	}
	if opts.environmentID != "" {
		body["environment_id"] = opts.environmentID
	}
	if opts.harness != "" {
		body["harness"] = opts.harness
	}
	if opts.model != "" {
		body["model"] = opts.model
	}
	if opts.effort != "" {
		body["effort"] = opts.effort
	}
	if opts.idleTimeout > 0 {
		body["idle_timeout_seconds"] = opts.idleTimeout
	}
}

func runChat(api *directoryAPI, reference, prompt string, opts taskOptions) error {
	agent, err := api.resolveChatAgent(reference)
	if err != nil {
		return err
	}
	if strings.TrimSpace(prompt) == "" {
		return errors.New("a prompt is required: pass it as an argument or pipe it on stdin")
	}

	identity, err := agentTaskIdentity(agent)
	if err != nil {
		return err
	}

	// created_by is the task's execution identity: the agent every commit,
	// pull request, and credential on this task resolves to. Passing the one
	// the caller selected is what keeps the explicit choice; the environment
	// it lands in and the responsible human stay the server's to decide.
	body := map[string]interface{}{
		"prompt":     prompt,
		"created_by": identity,
	}
	applyTaskOptions(body, opts)

	task, err := api.createTask(body)
	if err != nil {
		return err
	}

	return emitHandbook(map[string]interface{}{
		"data":      task,
		"agent":     agent,
		"mode":      "chat",
		"follow_up": taskFollowUp(task, nil),
	})
}

func runPlaybook(api *directoryAPI, reference, message, agentReference string, opts taskOptions) error {
	ref, err := api.resolveHandbookRef(reference, kindPlaybook)
	if err != nil {
		return err
	}

	// The tree node carries only a summary of the active version, so a
	// playbook that looks unpublished is checked against the record itself
	// before it is refused.
	if ref.ActiveVersion == nil {
		record, err := api.getPipeline(ref.ID)
		if err != nil {
			return handbookReadError(err, ref)
		}
		ref = refreshRef(ref, refFromPipelineRecord(record, api.accountID, ref.ResolvedFrom))
	}

	// Only the states the server itself refuses are refused here: `POST /tasks`
	// validates the playbook against the account, `archived_at`, and the active
	// version. A disabled playbook stays runnable by hand — disabling clears the
	// trigger routing so no event spawns a task, and a manual run is the
	// deliberate way to fire one — so it goes through as any other playbook does.
	if ref.State == "archived" {
		return errors.Errorf("playbook %s (%s) is %s and cannot be run", ref.Title, ref.ID, ref.State)
	}
	if ref.ActiveVersion == nil {
		return errors.Errorf("playbook %s (%s) has no published version, and a draft is not runnable; read it with `wallfacer handbook draft %s` and publish it first", ref.Title, ref.ID, ref.ID)
	}

	// No version is named in the request: the server runs the version it has
	// active, which is what "published" means here.
	body := map[string]interface{}{"pipeline_id": ref.ID}
	if strings.TrimSpace(message) != "" {
		body["message"] = message
	}

	var agent *teamMember
	if strings.TrimSpace(agentReference) != "" {
		agent, err = api.resolveChatAgent(agentReference)
		if err != nil {
			return err
		}
		identity, err := agentTaskIdentity(agent)
		if err != nil {
			return err
		}
		body["created_by"] = identity
	}
	applyTaskOptions(body, opts)

	task, err := api.createTask(body)
	if err != nil {
		return err
	}

	payload := map[string]interface{}{
		"data":     task,
		"playbook": ref,
		"mode":     "playbook",
		// Said plainly in the response as well as in the help, because the
		// one thing a caller must not read into `--agent` is that it moved
		// the work off the actors the published version names.
		"step_actors": "from the playbook's active published version; the server resolves each step's performer",
		"follow_up":   taskFollowUp(task, ref),
	}
	if ref.State == "disabled" {
		// Not a refusal: the run was created. Said plainly so a caller sees
		// they fired a playbook whose triggers are switched off and nothing
		// else will start it.
		payload["disabled_note"] = "the playbook is disabled: its triggers are cleared, so this manual run is the only way it starts; use handbook update-playbook --enable to restore the triggers"
	}
	if agent != nil {
		payload["agent"] = agent
	}
	return emitHandbook(payload)
}

// taskFollowUp names the command for each reference the created task hands
// back, so the session, message, and version reads are available from one
// result plus `--help` without this command growing a view of its own.
func taskFollowUp(task map[string]interface{}, playbook *handbookRef) map[string]interface{} {
	taskID := stringField(task, "id")

	out := map[string]interface{}{
		"task":     fmt.Sprintf("wallfacer tasks get %s", taskID),
		"sessions": fmt.Sprintf("wallfacer sessions list %s", taskID),
		"messages": fmt.Sprintf("wallfacer messages list %s <session-id>", taskID),
		"reply":    fmt.Sprintf("echo '{\"content\":\"...\"}' | wallfacer messages create %s <session-id>", taskID),
	}

	if playbook != nil {
		out["playbook"] = fmt.Sprintf("wallfacer handbook read %s", playbook.ID)
		if version := versionArgument(task["pipeline_version"]); version != "" {
			out["version"] = fmt.Sprintf("wallfacer handbook version %s %s", playbook.ID, version)
		} else {
			out["version"] = fmt.Sprintf("wallfacer handbook version %s", playbook.ID)
		}
	}

	return out
}

// promptFromStdin reads a piped prompt, so a long instruction arrives the way
// the low-level commands take their bodies rather than through the shell's
// quoting.
func promptFromStdin() (string, error) {
	info, err := os.Stdin.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice != 0 {
		return "", errors.New("a prompt is required: pass it as an argument or pipe it on stdin")
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", errors.Wrap(err, "Reading the prompt from stdin failed")
	}
	return strings.TrimSpace(string(data)), nil
}
