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

// version is set at build time via -ldflags "-X main.version=<tag>".
// Defaults to "dev" for local builds.
var version = "dev"

func configDir() string {
	home, _ := os.UserHomeDir()
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
			req.Header.Set("Authorization", "Bearer "+token)
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

	if baseURL := wfConfig.GetString("base_url"); baseURL != "" {
		viper.SetDefault("server", baseURL)
	}

	token := wfConfig.GetString("token")
	cli.Client.UseRequest(func(ctx *context.Context, h context.Handler) {
		if token != "" {
			ctx.Request.Header.Set("Authorization", "Bearer "+token)
		}
		h.Next(ctx)
	})

	accountID := wfConfig.GetString("account_id")

	registerAuthCommands(wfConfig.GetString("base_url"), token)
	openapiRegister(false)
	registerExecCommand(accountID)
	registerUpCommand(accountID)

	if accountID != "" {
		injectAccountID(cli.Root, accountID)
	}

	cli.Root.Execute()
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
