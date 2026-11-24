package main

import (
	"github.com/spf13/cobra"
)

// secretDeliveryHelp is appended to the generated `secrets create` help. That
// help is regenerated from the API spec and says how a secret is stored and
// referenced, but not where its value is delivered. An agent read it, stored
// API keys as environment secrets, put them in the manifest's env, and
// reported them available to its own shell, where they never arrive (#148).
// The text lives here rather than in openapi.go so `make generate` keeps it,
// and secrets_test.go fails if the routing guidance goes missing.
const secretDeliveryHelp = `Where the value goes:
  A secret is substituted only where the environment references it as
  ${secrets.NAME}: setup commands, services, named commands, mounts, and MCP
  server configuration. Manifest env is inherited by setup, services, and
  named commands. Neither creating a secret nor adding it to manifest env
  exports it into the sandbox shell where an agent runs its own commands.

Pick the mechanism by who uses the value:
  A service, setup step, or build      Environment secret (this command)
  A sandbox MCP server's tools         Environment secret in mcp_servers.<name>.env
  A remote MCP server                  Environment secret in mcp_servers.<name>.headers
  An agent's own shell commands/CLIs   Account keychain credential

The commands below assume a default account (WF_ACCOUNT_ID or account_id in
~/.wallfacer/wallfacer.yml). Without one, pass the account id first.

Environment secret:
  1. Store it, reading the value from stdin so it never appears on a
     command line or in shell history:
       jq -Rs '{value: rtrimstr("\n")}' < stripe.key \
         | wallfacer secrets create <environment-id> name: STRIPE_API_KEY
  2. Reference it, then send the manifest with
     wallfacer environments update <environment-id>:
       services:      "env": {"STRIPE_API_KEY": "${secrets.STRIPE_API_KEY}"}
       sandbox MCP:   "mcp_servers": {"<name>": {"command": "...",
                        "env": {"API_KEY": "${secrets.STRIPE_API_KEY}"}}}
     A tool that reads a file (.env, ~/.aws/credentials) gets nothing until a
     setup command or service command you write creates that file from the
     variable. No file is created for you.
  3. Timing: a manifest change regenerates the snapshot, and VMs booted
     after it is ready get the value. MCP server changes apply on the next
     session turn. Rotating a value (secrets update) does not regenerate the
     snapshot; run wallfacer snapshots create <environment-id> so setup and
     services pick it up.

Agent keychain credential:
  1. Store it on the account. The response carries the credential id and
     its reference, the variable name agents use (for example
     KEYCHAIN_X_API for a credential named "X API"):
       jq -Rs '{value: rtrimstr("\n")}' < x-api.key \
         | wallfacer credentials create name: "X API", kind: api_key
  2. Grant it to the agent (the agent's id is in wallfacer team list):
       wallfacer grants update <credential-id> <agent-id>
  3. Timing: the value arrives at the start of the agent's next session. A
     session already running does not get it.
  4. In that session, each command the agent runs sees $KEYCHAIN_X_API as a
     shell variable. It is not exported, so a program the command starts
     does not inherit it. Map it per command:
       X_API_KEY="$KEYCHAIN_X_API" node script.js

Verify delivery without revealing the value:
  Check presence in the consumer itself, never by printing the value or
  reading files the platform writes inside the VM:
    test -n "$KEYCHAIN_X_API" && echo present     (in the agent's session)
    a service health check, or a setup step such as test -n "$STRIPE_API_KEY"
    an authenticated call through the MCP server's own tool
  A saved manifest and a ready snapshot show configuration, not delivery.
  wallfacer credentials agent-list <agent-id> shows what an agent holds,
  metadata only. Do not add --verbose to a create call: it logs the request
  body, value included.

See also:
  wallfacer credentials create --help
  wallfacer grants update --help
  wallfacer environments update --help
  https://wallfacer.ai/docs/keychain
  https://wallfacer.ai/docs/api-environments`

// registerSecretDeliveryHelp appends secretDeliveryHelp to `secrets create`.
func registerSecretDeliveryHelp(root *cobra.Command) {
	create := findSubcommand(root, "secrets", "create")
	if create == nil {
		return
	}
	create.Long += "\n\n" + secretDeliveryHelp
}
