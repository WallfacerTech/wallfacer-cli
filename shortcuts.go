package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func registerExecCommand(accountID string) {
	cmd := &cobra.Command{
		Use:   "exec -- <command>",
		Short: "Execute a command in a VM",
		Long:  "Shortcut for 'wallfacer vms commands'. Everything after -- is the shell command to execute.",
		Args:  cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			vmID, _ := cmd.Flags().GetString("vm")
			if vmID == "" {
				fmt.Fprintln(os.Stderr, "Error: --vm is required")
				os.Exit(1)
			}
			if accountID == "" {
				fmt.Fprintln(os.Stderr, "Error: account_id not set in config. Run: wallfacer auth login")
				os.Exit(1)
			}

			dashIdx := cmd.ArgsLenAtDash()
			if dashIdx == -1 || dashIdx >= len(args) {
				fmt.Fprintln(os.Stderr, "Error: provide a command after --")
				os.Exit(1)
			}
			command := strings.Join(args[dashIdx:], " ")

			body := map[string]interface{}{
				"command": command,
			}
			if dir, _ := cmd.Flags().GetString("dir"); dir != "" {
				body["working_directory"] = dir
			}
			if timeout, _ := cmd.Flags().GetInt("timeout"); timeout > 0 {
				body["timeout"] = timeout
			}

			bodyBytes, err := json.Marshal(body)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			params := viper.New()
			_, decoded, err := OpenapiExecuteACommand(accountID, vmID, params, string(bodyBytes))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			if err := cli.Formatter.Format(decoded); err != nil {
				fmt.Fprintf(os.Stderr, "Formatting failed: %v\n", err)
				os.Exit(1)
			}
		},
	}
	cmd.Flags().String("vm", "", "VM ID (required)")
	cmd.Flags().String("dir", "", "Working directory for the command")
	cmd.Flags().Int("timeout", 0, "Maximum execution time in seconds (1-300)")

	cli.Root.AddCommand(cmd)
}

func registerUpCommand(accountID string) {
	cmd := &cobra.Command{
		Use:   "up <environment-id>",
		Short: "Wait for snapshot and start a VM",
		Long:  "Polls the environment until its base snapshot is ready, creates a VM, and waits for it to boot.",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			envID := args[0]
			if accountID == "" {
				fmt.Fprintln(os.Stderr, "Error: account_id not set in config. Run: wallfacer auth login")
				os.Exit(1)
			}

			pollInterval, _ := cmd.Flags().GetInt("poll")
			maxWait, _ := cmd.Flags().GetInt("max-wait")
			params := viper.New()

			// Phase 1: wait for snapshot ready
			fmt.Printf("Waiting for snapshot on environment %s...\n", envID)
			snapshotID, err := waitForSnapshot(accountID, envID, params, time.Duration(pollInterval)*time.Second, time.Duration(maxWait)*time.Second)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Snapshot ready: %s\n", snapshotID)

			// Phase 2: create VM with retry for propagation delay
			fmt.Println("Creating VM...")
			vmID, err := createVMWithRetry(accountID, envID, snapshotID, params)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating VM: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("VM created: %s\n", vmID)

			// Phase 3: wait for VM ready
			fmt.Println("Waiting for VM to boot...")
			vmData, err := waitForVMReady(accountID, vmID, params, time.Duration(pollInterval)*time.Second, time.Duration(maxWait)*time.Second)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			fmt.Printf("\nVM ready: %s\n", vmID)
			if ports, ok := vmData["port_mappings"].([]interface{}); ok {
				for _, p := range ports {
					if pm, ok := p.(map[string]interface{}); ok {
						fmt.Printf("  %-15s %s\n", pm["name"], pm["url"])
					}
				}
			}
		},
	}
	cmd.Flags().Int("poll", 5, "Poll interval in seconds")
	cmd.Flags().Int("max-wait", 600, "Maximum wait time in seconds")

	cli.Root.AddCommand(cmd)
}

func waitForSnapshot(accountID, envID string, params *viper.Viper, poll, maxWait time.Duration) (string, error) {
	deadline := time.Now().Add(maxWait)
	for {
		_, decoded, err := OpenapiGetAnEnvironment(accountID, envID, params)
		if err != nil {
			return "", fmt.Errorf("fetching environment: %w", err)
		}

		data, _ := decoded["data"].(map[string]interface{})
		if data == nil {
			return "", fmt.Errorf("unexpected response format")
		}

		snap, _ := data["base_snapshot"].(map[string]interface{})
		if snap != nil {
			status, _ := snap["status"].(string)
			id, _ := snap["id"].(string)
			if status == "ready" && id != "" {
				return id, nil
			}
			if status == "failed" {
				return "", fmt.Errorf("snapshot generation failed")
			}
			fmt.Printf("  snapshot status: %s\n", status)
		}

		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out after %s waiting for snapshot", maxWait)
		}
		time.Sleep(poll)
	}
}

func createVMWithRetry(accountID, envID, snapshotID string, params *viper.Viper) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"environment_id": envID,
		"snapshot_id":    snapshotID,
	})

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			delay := time.Duration(3*(attempt+1)) * time.Second
			fmt.Printf("  retrying in %s (snapshot propagation)...\n", delay)
			time.Sleep(delay)
		}

		_, decoded, err := OpenapiCreateAVM(accountID, params, string(body))
		if err != nil {
			lastErr = err
			if strings.Contains(err.Error(), "Snapshot not found") || strings.Contains(err.Error(), "404") {
				continue
			}
			return "", err
		}

		data, _ := decoded["data"].(map[string]interface{})
		if data != nil {
			if id, ok := data["id"].(string); ok {
				return id, nil
			}
		}
		return "", fmt.Errorf("unexpected response: missing VM id")
	}
	return "", fmt.Errorf("failed after retries: %w", lastErr)
}

func waitForVMReady(accountID, vmID string, params *viper.Viper, poll, maxWait time.Duration) (map[string]interface{}, error) {
	deadline := time.Now().Add(maxWait)
	for {
		_, decoded, err := OpenapiGetAVM(accountID, vmID, params)
		if err != nil {
			return nil, fmt.Errorf("fetching VM: %w", err)
		}

		data, _ := decoded["data"].(map[string]interface{})
		if data == nil {
			return nil, fmt.Errorf("unexpected response format")
		}

		status, _ := data["status"].(string)
		ready, _ := data["ready"].(bool)

		if ready {
			return data, nil
		}
		if status == "failed" || status == "stopped" {
			errMsg, _ := data["error"].(string)
			return nil, fmt.Errorf("VM entered %s state: %s", status, errMsg)
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out after %s waiting for VM (status: %s)", maxWait, status)
		}
		time.Sleep(poll)
	}
}
