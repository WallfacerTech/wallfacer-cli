package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Command runs are the tracked way to execute something on a VM: the record is
// written before the process starts, outlives the request that started it, and
// keeps the VM from being reclaimed as idle while the run is in flight. The
// generated `runs list/create/get/update` commands speak the endpoints
// directly; the shortcuts here add what a terminal wants on top of them —
// following a run's output as it is produced, and exiting with the remote
// command's exit code. `wallfacer exec` stays the quick ad hoc path for a
// command that answers inside one request.

// Exit codes for a run that ended without producing one of its own. A run that
// ran carries the remote command's exit code; these stand in for the cases
// where no process ever exited, and follow conventions a caller already reads
// (GNU `timeout` exits 124, a shell reports a SIGINT-killed process as 130).
const (
	exitRunCanceled = 130
	exitRunTimedOut = 124
	exitRunUnknown  = 1
)

// How long a read of a run holds the connection open waiting for the run to
// finish. Output arrives in bursts of at most this long, so it trades latency
// for request count; the API holds anything above its own maximum down to that.
const defaultRunWaitSeconds = 10

// commandRun is the subset of a run record these commands act on. Everything
// else in the record reaches the user through the generated commands, which
// print it verbatim.
type commandRun struct {
	ID                string     `json:"id"`
	Status            string     `json:"status"`
	ExitCode          *int       `json:"exit_code"`
	Error             string     `json:"error"`
	DurationMs        *int64     `json:"duration_ms"`
	CancelRequestedAt string     `json:"cancel_requested_at"`
	Output            *runOutput `json:"output"`
}

type runOutput struct {
	Text       string `json:"text"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	Truncated  bool   `json:"truncated"`
}

func registerRunCommands(accountID string) {
	group := runsGroup()

	startCmd := &cobra.Command{
		Use:   "start vm-id [-- <command>]",
		Short: "Start a command run in a VM",
		Long: "Starts a named command from the VM's manifest (`--name`), or an ad hoc command given after `--`, and prints the run.\n\n" +
			"With `--wait`, prints the run's output as it is produced and exits with the remote command's exit code instead. " +
			"A run that ends without one exits 124 when it timed out, 130 when it was canceled, and 1 otherwise.",
		Args: cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			vmID, command := splitRunTarget(cmd, args)
			requireRunAccount(accountID)

			name, _ := cmd.Flags().GetString("name")
			if (name == "") == (command == "") {
				exitRunError("provide either --name or a command after --, not both")
			}

			body := map[string]interface{}{}
			if name != "" {
				body["name"] = name
			}
			if command != "" {
				body["command"] = command
			}
			if dir, _ := cmd.Flags().GetString("dir"); dir != "" {
				body["working_directory"] = dir
			}
			if timeout, _ := cmd.Flags().GetInt("timeout"); timeout > 0 {
				body["timeout_seconds"] = timeout
			}
			pairs, _ := cmd.Flags().GetStringArray("env")
			env, err := parseRunEnv(pairs)
			if err != nil {
				exitRunError(err.Error())
			}
			if len(env) > 0 {
				body["env"] = env
			}

			wait, _ := cmd.Flags().GetBool("wait")
			waitSeconds, _ := cmd.Flags().GetInt("wait-seconds")
			if wait {
				// A run that finishes inside this window comes back complete,
				// with its output, in the response to the start itself.
				body["wait_seconds"] = waitSeconds
			}

			bodyBytes, err := json.Marshal(body)
			if err != nil {
				exitRunError(err.Error())
			}

			_, decoded, err := OpenapiStartACommandRun(accountID, vmID, viper.New(), string(bodyBytes))
			if err != nil {
				exitRunError(err.Error())
			}

			run, err := decodeRun(decoded)
			if err != nil {
				exitRunError(err.Error())
			}

			if !wait {
				if err := cli.Formatter.Format(decoded); err != nil {
					exitRunError(fmt.Sprintf("Formatting failed: %v", err))
				}
				return
			}

			cursor := writeRunOutput(run, 0, os.Stdout, os.Stderr)
			if !run.terminal() {
				poll, _ := cmd.Flags().GetInt("poll")
				run, err = followRun(accountID, vmID, run.ID, cursor, waitSeconds, poll, os.Stdout, os.Stderr)
				if err != nil {
					exitRunError(err.Error())
				}
			}
			finishRun(run)
		},
	}
	startCmd.Flags().String("name", "", "Name of a command declared in the VM's manifest")
	startCmd.Flags().String("dir", "", "Working directory for the command")
	startCmd.Flags().StringArray("env", nil, "Environment variable for this run only, as KEY=VALUE (repeatable)")
	startCmd.Flags().Int("timeout", 0, "Seconds the run may execute before it is killed (default 1800, max 14400)")
	startCmd.Flags().Bool("wait", false, "Follow the run's output and exit with the remote command's exit code")
	startCmd.Flags().Int("wait-seconds", defaultRunWaitSeconds, "Seconds each read waits for the run to finish; lower for smoother output, raise for fewer requests")
	startCmd.Flags().Int("poll", 1, "Seconds between reads while following")

	followCmd := &cobra.Command{
		Use:   "follow vm-id run-id",
		Short: "Follow a running command run",
		Long: "Prints a run's output from `--since` onwards as it is produced, then exits with the remote command's exit code. " +
			"A run that ends without one exits 124 when it timed out, 130 when it was canceled, and 1 otherwise.",
		Args: cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			requireRunAccount(accountID)

			since, _ := cmd.Flags().GetInt64("since")
			waitSeconds, _ := cmd.Flags().GetInt("wait-seconds")
			poll, _ := cmd.Flags().GetInt("poll")

			run, err := followRun(accountID, args[0], args[1], since, waitSeconds, poll, os.Stdout, os.Stderr)
			if err != nil {
				exitRunError(err.Error())
			}
			finishRun(run)
		},
	}
	followCmd.Flags().Int64("since", 0, "Byte offset to read output from (default 0, the start of the run)")
	followCmd.Flags().Int("wait-seconds", defaultRunWaitSeconds, "Seconds each read waits for the run to finish; lower for smoother output, raise for fewer requests")
	followCmd.Flags().Int("poll", 1, "Seconds between reads while following")

	cancelCmd := &cobra.Command{
		Use:   "cancel vm-id run-id",
		Short: "Cancel a command run",
		Long: "Stops a run and prints its record. Cancelling a run that has already finished returns it unchanged.\n\n" +
			"Exits 1 when the cancellation was recorded but could not be carried out, because the run has no confirmed process to signal. " +
			"That run keeps executing until its deadline; repeat the command to retry.",
		Args: cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			requireRunAccount(accountID)

			resp, decoded, err := OpenapiCancelACommandRun(accountID, args[0], args[1], viper.New(), `{"status":"canceled"}`)
			if err != nil {
				exitRunError(err.Error())
			}

			if err := cli.Formatter.Format(decoded); err != nil {
				exitRunError(fmt.Sprintf("Formatting failed: %v", err))
			}

			if resp.StatusCode == 202 {
				fmt.Fprintln(os.Stderr, "Cancellation recorded but not delivered: the run has no confirmed process to signal. It keeps executing until its deadline; repeat the command to retry.")
				os.Exit(1)
			}
		},
	}

	group.AddCommand(startCmd, followCmd, cancelCmd)
}

// runsGroup finds the generated `runs` group so the shortcuts sit alongside the
// commands they wrap. The group only exists when the spec the CLI was generated
// from carries the run endpoints, so it is created when it is missing rather
// than leaving the shortcuts unreachable.
func runsGroup() *cobra.Command {
	for _, cmd := range cli.Root.Commands() {
		if cmd.Name() == "runs" {
			return cmd
		}
	}

	group := &cobra.Command{
		Use:   "runs",
		Short: "Manage runs",
	}
	cli.Root.AddCommand(group)
	return group
}

// followRun reads a run from `since` onwards until it reaches a terminal
// status, writing each slice of output as it arrives.
func followRun(accountID, vmID, runID string, since int64, waitSeconds, pollSeconds int, out, errOut io.Writer) (*commandRun, error) {
	cursor := since

	for {
		params := viper.New()
		if cursor > 0 {
			params.Set("since", cursor)
		}
		if waitSeconds > 0 {
			params.Set("wait-seconds", waitSeconds)
		}

		_, decoded, err := OpenapiGetACommandRun(accountID, vmID, runID, params)
		if err != nil {
			return nil, err
		}

		run, err := decodeRun(decoded)
		if err != nil {
			return nil, err
		}

		cursor = writeRunOutput(run, cursor, out, errOut)
		if run.terminal() {
			return run, nil
		}

		// The read above already waited on the run itself; this only keeps a
		// server that answered immediately from being read in a tight loop.
		if pollSeconds > 0 {
			time.Sleep(time.Duration(pollSeconds) * time.Second)
		}
	}
}

// finishRun reports how a followed run ended and exits with the code it earned.
// The summary goes to stderr so stdout carries the command's output alone.
func finishRun(run *commandRun) {
	fmt.Fprintln(os.Stderr, runSummary(run))

	if code := runExitCode(run); code != 0 {
		os.Exit(code)
	}
}

// decodeRun reads the run out of an API response. The generated operations hand
// back the decoded body, so the run is re-marshalled into the fields these
// commands act on rather than being walked as a map.
func decodeRun(decoded map[string]interface{}) (*commandRun, error) {
	data, ok := decoded["data"]
	if !ok {
		return nil, fmt.Errorf("unexpected response: no run in body")
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("unexpected response: %w", err)
	}

	var run commandRun
	if err := json.Unmarshal(raw, &run); err != nil {
		return nil, fmt.Errorf("unexpected response: %w", err)
	}
	if run.ID == "" {
		return nil, fmt.Errorf("unexpected response: run has no id")
	}

	return &run, nil
}

// terminal reports whether the run is over. A status this CLI does not know is
// treated as terminal: ending a follow on it surfaces the status to the user,
// where waiting on it would hang until the run's deadline.
func (r *commandRun) terminal() bool {
	return r.Status != "pending" && r.Status != "running"
}

// writeRunOutput prints the slice of output a read returned and answers with
// the cursor the next read continues from. A read that returned no output
// leaves the cursor where it was.
func writeRunOutput(run *commandRun, cursor int64, out, errOut io.Writer) int64 {
	if run.Output == nil {
		return cursor
	}

	if run.Output.Truncated {
		fmt.Fprintf(errOut, "warning: output before byte %d was dropped; the run produced it faster than it was read\n", run.Output.Offset)
	}
	if run.Output.Text != "" {
		fmt.Fprint(out, run.Output.Text)
	}
	if run.Output.NextOffset > cursor {
		return run.Output.NextOffset
	}

	return cursor
}

func runExitCode(run *commandRun) int {
	if run.ExitCode != nil {
		return *run.ExitCode
	}

	switch run.Status {
	case "completed":
		return 0
	case "canceled":
		return exitRunCanceled
	case "timed_out":
		return exitRunTimedOut
	}

	return exitRunUnknown
}

func runSummary(run *commandRun) string {
	summary := fmt.Sprintf("run %s %s", run.ID, run.Status)

	if run.ExitCode != nil {
		summary += fmt.Sprintf(" (exit %d)", *run.ExitCode)
	}
	if run.DurationMs != nil {
		summary += fmt.Sprintf(" in %s", (time.Duration(*run.DurationMs) * time.Millisecond).Round(time.Millisecond))
	}
	if run.Error != "" {
		summary += ": " + run.Error
	}

	return summary
}

// parseRunEnv turns repeated KEY=VALUE flags into the object the API takes. An
// empty value is legitimate (`--env DEBUG=`); a missing `=` is not.
func parseRunEnv(pairs []string) (map[string]string, error) {
	env := map[string]string{}

	for _, pair := range pairs {
		key, value, found := strings.Cut(pair, "=")
		if !found || key == "" {
			return nil, fmt.Errorf("--env expects KEY=VALUE, got %q", pair)
		}
		env[key] = value
	}

	return env, nil
}

// splitRunTarget separates the VM id from the ad hoc command after `--`.
func splitRunTarget(cmd *cobra.Command, args []string) (vmID, command string) {
	positional := args

	if dashIdx := cmd.ArgsLenAtDash(); dashIdx >= 0 {
		positional = args[:dashIdx]
		command = strings.Join(args[dashIdx:], " ")
	}

	if len(positional) != 1 {
		exitRunError("provide exactly one VM ID")
	}

	return positional[0], command
}

func requireRunAccount(accountID string) {
	if accountID == "" {
		exitRunError("account_id not set in config. Run: wallfacer auth login")
	}
}

func exitRunError(message string) {
	fmt.Fprintf(os.Stderr, "Error: %s\n", message)
	os.Exit(1)
}
