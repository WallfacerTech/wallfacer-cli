package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/h2non/gentleman.v2/context"
	"gopkg.in/yaml.v2"
)

var version = "dev"

func configDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".wallfacer")
}

func configPath() string {
	return filepath.Join(configDir(), "wallfacer.yml")
}

func readConfig() map[string]interface{} {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return map[string]interface{}{}
	}
	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return map[string]interface{}{}
	}
	if cfg == nil {
		return map[string]interface{}{}
	}
	return cfg
}

func writeConfig(cfg map[string]interface{}) error {
	if err := os.MkdirAll(configDir(), 0700); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0600)
}

func registerAuthCommands(s resolved) {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication",
	}

	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Store an API token",
		Run: func(cmd *cobra.Command, args []string) {
			t, _ := cmd.Flags().GetString("token")
			if t == "" {
				fmt.Fprintln(os.Stderr, "Error: --token is required")
				os.Exit(1)
			}
			cfg := readConfig()
			setProfileField(cfg, s.profile, "token", t)

			// Default to the first account so commands work immediately after
			// login. Stored under the active profile (per-profile selection);
			// override per-command with --account-id or switch with
			// `wallfacer accounts use`.
			accounts, accErr := fetchAccounts(s.baseURL, t)
			if accErr == nil && len(accounts) > 0 {
				setProfileField(cfg, s.profile, "account_id", accounts[0].ID)
			}

			if err := writeConfig(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Token saved to " + configPath())

			switch {
			case accErr != nil:
				fmt.Fprintf(os.Stderr, "Note: couldn't fetch accounts to set a default (%v).\nSet one with: wallfacer accounts use <account-id>\n", accErr)
			case len(accounts) == 0:
				fmt.Println("No accounts found for this token.")
			default:
				fmt.Printf("Default account: %s  %s\n", accounts[0].ID, accounts[0].Name)
				if len(accounts) > 1 {
					fmt.Println("Other accounts:")
					for _, a := range accounts[1:] {
						fmt.Printf("  %s  %s\n", a.ID, a.Name)
					}
					fmt.Println("Switch with: wallfacer accounts use <account-id>")
				}
			}
		},
	}
	loginCmd.Flags().String("token", "", "API bearer token")

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Check authentication status",
		Run: func(cmd *cobra.Command, args []string) {
			serverURL := s.baseURL
			if serverURL == "" {
				serverURL = defaultServerURL
			}

			// Always report which profile/endpoint is being checked so the
			// active target is never ambiguous.
			if s.profile != "" {
				fmt.Printf("Profile: %s\n", s.profile)
			}
			fmt.Printf("Server:  %s\n", serverURL)

			if s.token == "" {
				fmt.Println("Not authenticated. Run: wallfacer auth login --token=<token>")
				os.Exit(1)
			}

			req, _ := http.NewRequest("GET", serverURL+"/v1/accounts", nil)
			req.Header.Set("Accept", "application/json")
			req.Header.Set("Authorization", "Bearer "+s.token)
			req.Header.Set("X-CLI-Version", version)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(resp.Body)

			if resp.StatusCode == 401 {
				fmt.Println("Token is invalid or expired.")
				os.Exit(1)
			}
			if resp.StatusCode != 200 {
				fmt.Fprintf(os.Stderr, "Unexpected status %d: %s\n", resp.StatusCode, string(body))
				os.Exit(1)
			}

			var result struct {
				Data []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"data"`
			}
			if err := json.Unmarshal(body, &result); err != nil {
				fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
				os.Exit(1)
			}

			fmt.Println("Authenticated. Accounts:")
			for _, a := range result.Data {
				marker := "  "
				if a.ID == s.accountID {
					marker = "* "
				}
				fmt.Printf("%s%s  %s\n", marker, a.ID, a.Name)
			}
			if s.accountID == "" {
				fmt.Println("\nNo default account set. Use `wallfacer accounts use <account-id>` or --account-id.")
			} else {
				fmt.Printf("\nActive account (*): %s\n", s.accountID)
			}
		},
	}

	logoutCmd := &cobra.Command{
		Use:   "logout",
		Short: "Remove stored API token",
		Run: func(cmd *cobra.Command, args []string) {
			cfg := readConfig()
			delete(cfg, "token")
			if err := writeConfig(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Token removed.")
		},
	}

	authCmd.AddCommand(loginCmd, statusCmd, logoutCmd)
	cli.Root.AddCommand(authCmd)
}

// firstEnv returns the first non-empty value among the named environment variables.
// Lets the CLI accept either the WALLFACER_ (human-facing, documented) or WF_
// (harness-injected; WF_ avoids Bunker's reserved WALLFACER_ manifest-env prefix)
// spelling of the same credential. WALLFACER_ wins when both are set.
func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func main() {
	cli.Init(&cli.Config{
		AppName:   "wallfacer",
		EnvPrefix: "WALLFACER",
		Version:   version,
	})

	wfConfig := viper.New()
	wfConfig.SetConfigName("wallfacer")
	wfConfig.SetConfigType("yaml")
	wfConfig.AddConfigPath(configDir())
	wfConfig.ReadInConfig()

	// Auth resolution: ~/.wallfacer/wallfacer.yml first, then environment.
	// The file wins for human users on their own machines; the env fallback
	// (either the `WALLFACER_` or `WF_` spelling; `WALLFACER_` wins) lets
	// ephemeral environments (e.g. Wallfacer harness VMs) inject credentials with
	// no file present. Env only fills a field when it is absent from the file.
	// Register the --profile global flag so cobra accepts it on every command.
	// Its value is also pre-scanned directly from os.Args during settings
	// resolution, because account-id injection below rewrites command
	// definitions before cobra ever parses flags.
	cli.AddGlobalFlag("profile", "", "Configuration profile to use (run 'wallfacer profile list')", "")

	// Per-command account override. Highest-precedence account selector; like
	// --profile it is also pre-scanned from os.Args during settings resolution
	// because account-id injection runs before cobra parses flags.
	cli.AddGlobalFlag("account-id", "", "Account to target for this command (overrides the configured default)", "")

	// Resolve connection settings: an active profile (--profile /
	// WALLFACER_PROFILE), else the legacy top-level keys, each with the
	// WALLFACER_/WF_ env vars as a per-field fallback.
	settings, err := resolveSettings(wfConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if settings.baseURL != "" {
		viper.SetDefault("server", settings.baseURL)
	}

	token := settings.token
	cli.Client.UseRequest(func(ctx *context.Context, h context.Handler) {
		ctx.Request.Header.Set("Accept", "application/json")
		ctx.Request.Header.Set("X-CLI-Version", version)
		if token != "" {
			ctx.Request.Header.Set("Authorization", "Bearer "+token)
		}
		h.Next(ctx)
	})

	accountID := settings.accountID

	registerAuthCommands(settings)
	registerProfileCommands(wfConfig, settings)
	openapiRegister(false)
	registerExecCommand(accountID)
	registerUpCommand(accountID)

	if accountID != "" {
		injectAccountID(cli.Root, accountID)
	}

	// Registered after injectAccountID: its Use string "use <account-id>"
	// contains the "account-id" substring the injector matches on, so adding it
	// earlier would get the command rewritten and the positional stripped.
	registerAccountSelectionCommands(settings)

	updateCh := startUpdateCheck(version)
	cli.Root.Execute()
	select {
	case result := <-updateCh:
		if result != nil {
			printUpdateMessage(result)
		}
	default:
	}
}

func injectAccountID(parent *cobra.Command, accountID string) {
	for _, cmd := range parent.Commands() {
		if cmd.HasSubCommands() {
			injectAccountID(cmd, accountID)
			continue
		}
		if cmd.Run == nil {
			continue
		}
		if strings.Contains(cmd.Use, "account-id") {
			capturedRun := cmd.Run
			cmd.Run = func(c *cobra.Command, args []string) {
				capturedRun(c, append([]string{accountID}, args...))
			}
			cmd.Args = cobra.MinimumNArgs(0)
			cmd.Use = strings.Replace(cmd.Use, "account-id ", "", 1)
			cmd.Use = strings.Replace(cmd.Use, "account-id", "", 1)
		}
	}
}
