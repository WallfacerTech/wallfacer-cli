package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type apiAccount struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// fetchAccounts lists the accounts reachable with the given token via a raw
// request. Callers such as `auth login` need this because the token in hand is
// not yet the one bound to the shared client.
func fetchAccounts(serverURL, token string) ([]apiAccount, error) {
	if serverURL == "" {
		serverURL = defaultServerURL
	}
	req, _ := http.NewRequest("GET", serverURL+"/v1/accounts", nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-CLI-Version", version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var result struct {
		Data []apiAccount `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result.Data, nil
}

// setProfileField stores a single connection field (token, account_id, …) in
// cfg. With an active profile the value is written under
// profiles.<name>.<field>; otherwise it is a top-level key. yaml.v2 decodes
// nested maps as map[interface{}]interface{}, so both that and the
// map[string]interface{} shape are normalised on the way through.
func setProfileField(cfg map[string]interface{}, profile, field, value string) {
	if profile == "" {
		cfg[field] = value
		return
	}

	profiles := toStringMap(cfg["profiles"])
	if profiles == nil {
		profiles = map[string]interface{}{}
	}

	// Preserve the existing key spelling if the profile already exists.
	key := profile
	var prof map[string]interface{}
	for k, v := range profiles {
		if strings.EqualFold(k, profile) {
			key = k
			prof = toStringMap(v)
			break
		}
	}
	if prof == nil {
		prof = map[string]interface{}{}
	}

	prof[field] = value
	profiles[key] = prof
	cfg["profiles"] = profiles
}

// toStringMap normalises a yaml-decoded map (which may be keyed by interface{})
// to a string-keyed map. Returns nil for anything that is not a map.
func toStringMap(v interface{}) map[string]interface{} {
	switch m := v.(type) {
	case map[string]interface{}:
		return m
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(m))
		for k, val := range m {
			out[fmt.Sprint(k)] = val
		}
		return out
	}
	return nil
}

// registerAccountSelectionCommands attaches `use` and `current` to the
// generated `accounts` group. It MUST run after openapiRegister (so the group
// exists) and after injectAccountID (whose substring match on "account-id"
// would otherwise rewrite `use <account-id>`).
func registerAccountSelectionCommands(active resolved) {
	accountsCmd := findCommand(cli.Root, "accounts")
	if accountsCmd == nil {
		return
	}

	useCmd := &cobra.Command{
		Use:   "use <account-id>",
		Short: "Set the default account for later commands",
		Long: cli.Markdown("Persists `<account-id>` as the default account used by account-scoped commands.\n\n" +
			"When a profile is active (`--profile` / `WALLFACER_PROFILE`) the default is stored under that profile; " +
			"otherwise it is the top-level default. Override it for a single command at any time with `--account-id`."),
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			id := args[0]
			cfg := readConfig()
			setProfileField(cfg, active.profile, "account_id", id)
			if err := writeConfig(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing config: %v\n", err)
				os.Exit(1)
			}
			if active.profile != "" {
				fmt.Printf("Default account for profile %q set to %s\n", active.profile, id)
			} else {
				fmt.Printf("Default account set to %s\n", id)
			}
		},
	}

	currentCmd := &cobra.Command{
		Use:   "current",
		Short: "Show the account that commands target right now",
		Run: func(cmd *cobra.Command, args []string) {
			if active.profile != "" {
				fmt.Printf("Profile:    %s\n", active.profile)
			}
			if active.accountID == "" {
				fmt.Println("Account ID: — (none selected)")
				fmt.Println("Set one with `wallfacer accounts use <account-id>` or pass --account-id.")
				return
			}
			fmt.Printf("Account ID: %s\n", active.accountID)
			if name := lookupAccountName(active.accountID); name != "" {
				fmt.Printf("Name:       %s\n", name)
			}
		},
	}

	accountsCmd.AddCommand(useCmd, currentCmd)
}

// lookupAccountName best-effort resolves a friendly account name through the
// configured client; returns "" on any error so callers can simply omit it.
func lookupAccountName(id string) string {
	_, decoded, err := OpenapiListAccounts(viper.New())
	if err != nil {
		return ""
	}
	data, _ := decoded["data"].([]interface{})
	for _, item := range data {
		m, _ := item.(map[string]interface{})
		if m != nil && fmt.Sprint(m["id"]) == id {
			if n, ok := m["name"].(string); ok {
				return n
			}
		}
	}
	return ""
}

func findCommand(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}
