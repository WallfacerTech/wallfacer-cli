package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/spf13/cobra"
)

// secretsCreateHelp renders `secrets create --help` from the real generated
// commands, with the delivery guidance added the way main adds it.
func secretsCreateHelp(t *testing.T) string {
	t.Helper()
	previous := cli.Root
	cli.Root = &cobra.Command{Use: "wallfacer"}
	t.Cleanup(func() { cli.Root = previous })
	registerGenerated(cli.Root, func() { openapiRegister(false) })
	registerSecretDeliveryHelp(cli.Root)

	var out bytes.Buffer
	cli.Root.SetOutput(&out)
	cli.Root.SetArgs([]string{"secrets", "create", "--help"})
	if err := cli.Root.Execute(); err != nil {
		t.Fatalf("secrets create --help: %v", err)
	}
	return out.String()
}

// A reader starting from this help alone has to be able to tell an environment
// secret from an agent credential, finish either workflow, know when it takes
// effect, and check delivery without printing the value (#148).
func TestSecretsCreateHelpRoutesCredentialsToTheRightMechanism(t *testing.T) {
	help := secretsCreateHelp(t)

	for _, want := range []string{
		// The generated description is kept, not replaced.
		"Encrypts and stores a new secret",
		// Delivery is by reference only, and never to the agent's shell.
		"${secrets.NAME}",
		"exports it into the sandbox shell",
		// Routing, including MCP servers.
		"Account keychain credential",
		"mcp_servers.<name>.env",
		// Agent keychain workflow, reference, timing, and per-command mapping.
		"wallfacer credentials create",
		"wallfacer grants update <credential-id> <agent-id>",
		"KEYCHAIN_X_API",
		"agent's next session",
		"It is not exported",
		`X_API_KEY="$KEYCHAIN_X_API" node script.js`,
		// Files a tool reads are the user's to create.
		"No file is created for you",
		// Safe input and presence-only verification.
		"jq -Rs",
		`test -n "$KEYCHAIN_X_API" && echo present`,
		// Where to read further.
		"wallfacer credentials create --help",
		"wallfacer grants update --help",
		"wallfacer environments update --help",
		"https://wallfacer.ai/docs/keychain",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("secrets create --help is missing %q", want)
		}
	}

	// Verification must not teach printing a value or reading the VM's own
	// manifest.
	for _, banned := range []string{"agent-env.json", `echo "$KEYCHAIN`, "echo $KEYCHAIN", "printenv", "cat ~/.wallfacer"} {
		if strings.Contains(help, banned) {
			t.Errorf("secrets create --help suggests %q", banned)
		}
	}
}
