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

func registerAuthCommands(baseURL, token string) {
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
			cfg["token"] = t
			if err := writeConfig(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Token saved to " + configPath())
		},
	}
	loginCmd.Flags().String("token", "", "API bearer token")

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Check authentication status",
		Run: func(cmd *cobra.Command, args []string) {
			if token == "" {
				fmt.Println("Not authenticated. Run: wallfacer auth login --token=<token>")
				os.Exit(1)
			}

			serverURL := baseURL
			if serverURL == "" {
				serverURL = "https://api.wallfacer.ai"
			}

			req, _ := http.NewRequest("GET", serverURL+"/v1/accounts", nil)
			req.Header.Set("Accept", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
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
				fmt.Printf("  %s  %s\n", a.ID, a.Name)
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
	baseURL := wfConfig.GetString("base_url")
	if baseURL == "" {
		baseURL = firstEnv("WALLFACER_SERVER", "WF_SERVER")
	}
	if baseURL != "" {
		viper.SetDefault("server", baseURL)
	}

	token := wfConfig.GetString("token")
	if token == "" {
		token = firstEnv("WALLFACER_TOKEN", "WF_TOKEN")
	}
	cli.Client.UseRequest(func(ctx *context.Context, h context.Handler) {
		ctx.Request.Header.Set("Accept", "application/json")
		ctx.Request.Header.Set("X-CLI-Version", version)
		if token != "" {
			ctx.Request.Header.Set("Authorization", "Bearer "+token)
		}
		h.Next(ctx)
	})

	accountID := wfConfig.GetString("account_id")
	if accountID == "" {
		accountID = firstEnv("WALLFACER_ACCOUNT_ID", "WF_ACCOUNT_ID")
	}

	registerAuthCommands(baseURL, token)
	openapiRegister(false)
	registerExecCommand(accountID)
	registerUpCommand(accountID)
	registerRunCommands(accountID)

	if accountID != "" {
		injectAccountID(cli.Root, accountID)
	}

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
