package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// resolved holds the connection settings after profile + environment resolution.
// profile is the active profile name ("" means legacy top-level keys).
type resolved struct {
	profile   string
	baseURL   string
	token     string
	accountID string
}

// profileName returns the active profile name, resolved from (in order):
//  1. the --profile flag, pre-scanned directly from os.Args (account-id
//     injection rewrites command definitions before cobra parses flags, so we
//     cannot wait for the parsed flag value here)
//  2. the WALLFACER_PROFILE / WF_PROFILE environment variable
//
// An empty string means no profile is selected: the legacy top-level config
// keys are used, exactly as the CLI behaved before profiles existed.
func profileName() string {
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			// End-of-flags marker (e.g. `exec -- <command>`). Anything after
			// this belongs to the command body, not to wallfacer's flags.
			break
		}
		switch {
		case a == "--profile":
			if i+1 < len(args) {
				return args[i+1]
			}
		case strings.HasPrefix(a, "--profile="):
			return strings.TrimPrefix(a, "--profile=")
		}
	}
	return firstEnv("WALLFACER_PROFILE", "WF_PROFILE")
}

// resolveSettings resolves base_url, token, and account_id.
//
// When a profile is active, its fields are the authoritative "file" values; the
// WALLFACER_/WF_ env vars only fill a field the profile leaves empty. When no
// profile is active, the legacy top-level keys play that role. This preserves
// the documented file-first, env-fallback contract — a profile is simply a
// named set of file values. A profile never falls back to the top-level keys:
// those belong to the implicit default, and bleeding (say) a prod account_id
// into a local profile would be a footgun.
func resolveSettings(wfConfig *viper.Viper) (resolved, error) {
	name := profileName()

	var fileVal func(field string) string
	if name == "" {
		fileVal = func(field string) string { return wfConfig.GetString(field) }
	} else {
		profiles := wfConfig.GetStringMap("profiles")
		if _, ok := profiles[strings.ToLower(name)]; !ok {
			return resolved{}, fmt.Errorf(
				"profile %q not found in %s (run `wallfacer profile list`)", name, configPath())
		}
		// Dotted access reads nested values robustly regardless of the
		// underlying YAML map type, and viper keys are case-insensitive.
		prefix := "profiles." + strings.ToLower(name) + "."
		fileVal = func(field string) string { return wfConfig.GetString(prefix + field) }
	}

	pick := func(field string, envKeys ...string) string {
		if v := fileVal(field); v != "" {
			return v
		}
		return firstEnv(envKeys...)
	}

	return resolved{
		profile:   name,
		baseURL:   pick("base_url", "WALLFACER_SERVER", "WF_SERVER"),
		token:     pick("token", "WALLFACER_TOKEN", "WF_TOKEN"),
		accountID: pick("account_id", "WALLFACER_ACCOUNT_ID", "WF_ACCOUNT_ID"),
	}, nil
}

const defaultServerURL = "https://api.wallfacer.ai"

// registerProfileCommands adds the read-only `profile` command group used to
// discover configured profiles and inspect the active connection settings.
func registerProfileCommands(wfConfig *viper.Viper, active resolved) {
	profileCmd := &cobra.Command{
		Use:   "profile",
		Short: "Inspect configuration profiles",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List configured profiles",
		Run: func(cmd *cobra.Command, args []string) {
			profiles := wfConfig.GetStringMap("profiles")
			if len(profiles) == 0 {
				fmt.Println("No profiles defined. Using top-level keys (default).")
				fmt.Printf("Add a `profiles:` block to %s to define some.\n", configPath())
				return
			}

			names := make([]string, 0, len(profiles))
			for n := range profiles {
				names = append(names, n)
			}
			sort.Strings(names)

			for _, n := range names {
				marker := "  "
				if strings.EqualFold(n, active.profile) {
					marker = "* "
				}
				server := wfConfig.GetString("profiles." + n + ".base_url")
				if server == "" {
					server = defaultServerURL + " (default)"
				}
				fmt.Printf("%s%-14s %s\n", marker, n, server)
			}
		},
	}

	currentCmd := &cobra.Command{
		Use:   "current",
		Short: "Show the active profile and resolved connection settings",
		Run: func(cmd *cobra.Command, args []string) {
			name := active.profile
			if name == "" {
				name = "(none — top-level default)"
			}
			server := active.baseURL
			if server == "" {
				server = defaultServerURL + " (default)"
			}
			tokenState := "not set"
			if active.token != "" {
				tokenState = "set"
			}
			fmt.Printf("Profile:    %s\n", name)
			fmt.Printf("Server:     %s\n", server)
			fmt.Printf("Account ID: %s\n", dashIfEmpty(active.accountID))
			fmt.Printf("Token:      %s\n", tokenState)
		},
	}

	profileCmd.AddCommand(listCmd, currentCmd)
	cli.Root.AddCommand(profileCmd)
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
