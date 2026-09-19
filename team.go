package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// The directory holds two kinds of record, listed by two different endpoints.
// Only one of them can be given work, so the type travels with every record
// rather than being inferred from a name.
const (
	memberAgent = "agent"
	memberHuman = "human"
)

// registerTeamCommands adds the top-level `team` surface: who is in the
// account, which of them are agents, and the IDs the existing commands take.
// It reads through the existing account-scoped agent and user endpoints; the
// generated `agents` and `users` groups keep working unchanged.
func registerTeamCommands(accountID string) {
	teamCmd := &cobra.Command{
		Use:   "team",
		Short: "Find the agents and people in the account",
		Long: cli.Markdown(`Discover the account's team: its agents and its human members in one list.

Every command here is a read, and the directory never aggregates a member's work.
Each record carries the IDs the existing commands already take, plus a ` + "`follow_up`" + `
naming them: an agent's role page reads with ` + "`wallfacer handbook read <role-page-id>`" + `,
and a member's tasks list with ` + "`wallfacer tasks list --created-by <agent-id>`" + ` or
` + "`--owner-user-id <user-id>`" + `.

Commands that take a ` + "`<reference>`" + ` accept a member ID, a unique display name, an
agent handle (with or without a leading ` + "`@`" + `), or an email address.`),
	}

	teamCmd.AddCommand(
		teamListCommand(accountID),
		teamGetCommand(accountID),
	)

	cli.Root.AddCommand(teamCmd)
}

// teamRun wires a command body to the account, matching the error handling of
// the other product commands: print to stderr and exit non-zero.
func teamRun(accountID string, body func(api *directoryAPI, cmd *cobra.Command, args []string) error) func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, args []string) {
		api, err := newDirectoryAPI(accountID)
		if err == nil {
			err = body(api, cmd, args)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}
}

func teamListCommand(accountID string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the account's agents and human members",
		Long:  cli.Markdown("Sweeps both listings past the first page and returns one list carrying each record's type, identity, role data, and state. Offboarded agents are left out unless `--include-disabled` is passed; `pagination` reports how far each sweep got."),
		Args:  cobra.NoArgs,
		Run: teamRun(accountID, func(api *directoryAPI, cmd *cobra.Command, args []string) error {
			kind, err := teamKindFlag(cmd)
			if err != nil {
				return err
			}
			includeDisabled, _ := cmd.Flags().GetBool("include-disabled")
			maxPages, _ := cmd.Flags().GetInt("max-pages")
			return runTeamList(api, kind, includeDisabled, maxPages)
		}),
	}
	addTeamKindFlag(cmd)
	cmd.Flags().Bool("include-disabled", false, "Include offboarded (disabled) agents")
	cmd.Flags().Int("max-pages", 20, "Maximum pages to read from each underlying listing")
	return cmd
}

func teamGetCommand(accountID string) *cobra.Command {
	return &cobra.Command{
		Use:   "get <reference>",
		Short: "Resolve one reference to a single member, with its type and state",
		Long:  cli.Markdown("Resolves an ID, display name, agent handle, or email to exactly one record, and reports whether it is an agent or a human and whether it can take work. Every candidate is reported when more than one matches, rather than one being guessed at. Reading the directory never creates a task."),
		Args:  cobra.ExactArgs(1),
		Run: teamRun(accountID, func(api *directoryAPI, cmd *cobra.Command, args []string) error {
			return runTeamGet(api, args[0])
		}),
	}
}

func addTeamKindFlag(cmd *cobra.Command) {
	cmd.Flags().String("type", "", "Restrict to one record type: agent or human")
}

func teamKindFlag(cmd *cobra.Command) (string, error) {
	value, _ := cmd.Flags().GetString("type")
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", nil
	case memberAgent, "agents":
		return memberAgent, nil
	case memberHuman, "humans", "user", "users", "member", "members", "people":
		return memberHuman, nil
	default:
		return "", errors.Errorf("--type must be agent or human, got %q", value)
	}
}

// directoryAPI reads the account's team directory and starts the work a
// reference points at. It embeds handbookAPI for the account-scoped request
// plumbing and the shared handbook reference resolver, which is what lets `run`
// accept exactly the playbook references `handbook` does.
type directoryAPI struct {
	*handbookAPI

	// directory caches both listings swept in full. Resolution runs against
	// it, so it is worth reading once per invocation.
	directory []*teamMember
}

func newDirectoryAPI(accountID string) (*directoryAPI, error) {
	handbook, err := newHandbookAPI(accountID)
	if err != nil {
		return nil, err
	}
	return &directoryAPI{handbookAPI: handbook}, nil
}

func (a *directoryAPI) listAgents(query url.Values) (map[string]interface{}, error) {
	return a.get(a.accountPath("/agents"), query)
}

func (a *directoryAPI) listUsers(query url.Values) (map[string]interface{}, error) {
	return a.get(a.accountPath("/users"), query)
}

// createTask is the only write these commands make. Both `chat` and `run` go
// through it, with the body each of them built, so the two execution modes stay
// distinguishable in one place and a rejected request surfaces the API's own
// error text.
func (a *directoryAPI) createTask(body map[string]interface{}) (map[string]interface{}, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	server := viper.GetString("server")
	if server == "" {
		server = openapiServers()[viper.GetInt("server-index")]["url"]
	}

	resp, err := cli.Client.Post().URL(server+a.accountPath("/tasks")).
		AddHeader("Content-Type", "application/json").
		BodyString(string(encoded)).
		Do()
	if err != nil {
		return nil, errors.Wrap(err, "Request failed")
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, errors.Errorf("HTTP %d: %s", resp.StatusCode, resp.String())
	}

	var decoded map[string]interface{}
	if err := cli.UnmarshalResponse(resp, &decoded); err != nil {
		return nil, errors.Wrap(err, "Unmarshalling response failed")
	}
	return responseObject(decoded)
}

// teamMember is one directory record of either kind, flattened to the fields a
// caller picks a teammate by and the IDs the next command takes.
type teamMember struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Handle string `json:"handle,omitempty"`
	Email  string `json:"email,omitempty"`

	Title          string `json:"title,omitempty"`
	Role           string `json:"role,omitempty"`
	RolePageID     string `json:"role_page_id,omitempty"`
	EnvironmentID  string `json:"environment_id,omitempty"`
	GithubUsername string `json:"github_username,omitempty"`
	Vendor         string `json:"vendor,omitempty"`
	Model          string `json:"model,omitempty"`
	RuntimeStatus  string `json:"runtime_status,omitempty"`

	State        string            `json:"state"`
	Chatable     bool              `json:"chatable"`
	ResolvedFrom string            `json:"resolved_from,omitempty"`
	FollowUp     map[string]string `json:"follow_up,omitempty"`
}

func memberFromAgentRecord(record map[string]interface{}) *teamMember {
	member := &teamMember{
		Type:           memberAgent,
		ID:             identifierField(record, "id"),
		Name:           stringField(record, "display_name"),
		Handle:         stringField(record, "handle"),
		Email:          stringField(record, "email"),
		Title:          stringField(record, "title"),
		RolePageID:     stringField(record, "role_page_id"),
		EnvironmentID:  stringField(record, "environment_id"),
		GithubUsername: stringField(record, "github_username"),
		Vendor:         stringField(record, "vendor"),
		Model:          stringField(record, "model"),
		State:          "active",
	}
	if runtime, ok := record["runtime"].(map[string]interface{}); ok {
		member.RuntimeStatus = stringField(runtime, "status")
	}
	if member.Name == "" {
		member.Name = member.Handle
	}

	switch {
	case boolField(record, "disabled"):
		member.State = "disabled"
	case boolField(record, "paused"):
		member.State = "paused"
	}

	// An offboarded or paused agent is still in the directory, so it can be
	// named in a refusal; it just cannot be handed work.
	member.Chatable = member.State == "active"
	member.FollowUp = memberFollowUp(member)
	return member
}

func memberFromUserRecord(record map[string]interface{}) *teamMember {
	member := &teamMember{
		Type:           memberHuman,
		ID:             identifierField(record, "id"),
		Name:           stringField(record, "name"),
		Email:          stringField(record, "email"),
		Role:           stringField(record, "role"),
		GithubUsername: stringField(record, "github_username"),
		// The members listing returns active memberships only: a removed human
		// is absent from it rather than reported with a state of their own.
		State: "active",
	}
	member.FollowUp = memberFollowUp(member)
	return member
}

// memberFollowUp names the command for each reference the record hands back.
// The directory reports no task counts of its own: it points at the task list's
// existing filters instead, so one surface owns that read.
func memberFollowUp(member *teamMember) map[string]string {
	out := map[string]string{}

	switch member.Type {
	case memberAgent:
		out["tasks"] = fmt.Sprintf("wallfacer tasks list --created-by %s", member.ID)
		if member.RolePageID != "" {
			out["role_page"] = fmt.Sprintf("wallfacer handbook read %s", member.RolePageID)
		}
		if member.Chatable {
			out["chat"] = fmt.Sprintf("wallfacer chat %s \"<what you need>\"", member.ID)
		}
	case memberHuman:
		out["tasks"] = fmt.Sprintf("wallfacer tasks list --owner-user-id %s", member.ID)
	}

	return out
}

// ambiguousMemberError reports every candidate a reference matched, so the
// caller can pick one instead of the CLI guessing which teammate was meant.
type ambiguousMemberError struct {
	reference  string
	candidates []*teamMember
}

func (e *ambiguousMemberError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches %d team records; use an ID or a handle:", e.reference, len(e.candidates))
	for _, c := range e.candidates {
		handle := c.Handle
		if handle == "" {
			handle = c.Email
		}
		fmt.Fprintf(&b, "\n  %-6s  %-4s  %s  %s", c.Type, c.ID, c.Name, handle)
	}
	return b.String()
}

// loadDirectory reads both listings in full, disabled agents included.
// Resolution has to be exhaustive: an agent that happens to sort onto the
// second page is still the agent the caller named, and a name that looks unique
// only because the sweep stopped early is worse than no answer at all.
func (a *directoryAPI) loadDirectory() ([]*teamMember, error) {
	if a.directory != nil {
		return a.directory, nil
	}
	members, _, err := a.readDirectory("", true, sweepUnbounded)
	if err != nil {
		return nil, err
	}
	if members == nil {
		members = []*teamMember{}
	}
	a.directory = members
	return members, nil
}

// readDirectory sweeps the agent and user listings and reports how far each
// sweep got, alongside the records themselves. The two listings paginate
// differently: agents is cursor-paginated, users is offset-paginated.
func (a *directoryAPI) readDirectory(kind string, includeDisabled bool, maxPages int) ([]*teamMember, map[string]interface{}, error) {
	members := []*teamMember{}
	pagination := map[string]interface{}{}

	if kind == "" || kind == memberAgent {
		sweep, err := a.sweepCursor(func(query url.Values) (map[string]interface{}, error) {
			if includeDisabled {
				query.Set("include_disabled", "true")
			}
			return a.listAgents(query)
		}, maxPages, func(record map[string]interface{}) bool {
			members = append(members, memberFromAgentRecord(record))
			return true
		})
		if err != nil {
			return nil, nil, err
		}
		pagination["agents"] = sweep
	}

	if kind == "" || kind == memberHuman {
		sweep, err := a.sweep(a.listUsers, maxPages, func(record map[string]interface{}) bool {
			members = append(members, memberFromUserRecord(record))
			return true
		})
		if err != nil {
			return nil, nil, err
		}
		pagination["humans"] = sweep
	}

	return members, pagination, nil
}

// resolveTeamMember turns a reference into exactly one directory record.
//
// The forms are tried most specific first: an ID is unambiguous, a handle and
// an email are unique within an account, and a display name is neither. A tier
// that matches more than one record reports all of them rather than picking.
func (a *directoryAPI) resolveTeamMember(reference string) (*teamMember, error) {
	trimmed := strings.TrimSpace(reference)
	if trimmed == "" {
		return nil, errors.New("a team reference is required")
	}

	members, err := a.loadDirectory()
	if err != nil {
		return nil, err
	}

	handle := strings.TrimPrefix(trimmed, "@")

	tiers := []struct {
		from  string
		match func(*teamMember) bool
	}{
		{"id", func(m *teamMember) bool { return m.ID != "" && m.ID == trimmed }},
		{"handle", func(m *teamMember) bool { return m.Handle != "" && strings.EqualFold(m.Handle, handle) }},
		{"email", func(m *teamMember) bool { return m.Email != "" && strings.EqualFold(m.Email, trimmed) }},
		{"name", func(m *teamMember) bool { return m.Name != "" && strings.EqualFold(m.Name, trimmed) }},
	}

	for _, tier := range tiers {
		var matches []*teamMember
		for _, member := range members {
			if tier.match(member) {
				matches = append(matches, member)
			}
		}

		switch len(matches) {
		case 0:
			continue
		case 1:
			resolved := *matches[0]
			resolved.ResolvedFrom = tier.from
			return &resolved, nil
		default:
			return nil, &ambiguousMemberError{reference: trimmed, candidates: matches}
		}
	}

	return nil, errors.Errorf("no agent or member matching %q in account %s; list the directory with `wallfacer team list`", trimmed, a.accountID)
}

// resolveChatAgent narrows a reference to one agent that can actually be given
// work. A human member and a disabled or paused agent are each refused by name
// rather than passed through to task create, so an explicit selection never
// quietly becomes somebody else's identity.
func (a *directoryAPI) resolveChatAgent(reference string) (*teamMember, error) {
	member, err := a.resolveTeamMember(reference)
	if err != nil {
		return nil, err
	}

	if member.Type != memberAgent {
		return nil, errors.Errorf("%q is %s, a human member of this account, and cannot be the agent on a task; pick one with `wallfacer team list --type agent`", reference, member.Name)
	}
	if !member.Chatable {
		return nil, errors.Errorf("agent %s (%s) is %s and cannot take work; pick an enabled agent with `wallfacer team list --type agent`", member.Name, member.ID, member.State)
	}
	return member, nil
}

// agentTaskIdentity renders a resolved agent as the integer the task endpoint
// takes for `created_by`.
func agentTaskIdentity(member *teamMember) (int64, error) {
	id, err := strconv.ParseInt(member.ID, 10, 64)
	if err != nil {
		return 0, errors.Errorf("agent %s has an unusable ID %q", member.Name, member.ID)
	}
	return id, nil
}

func runTeamList(api *directoryAPI, kind string, includeDisabled bool, maxPages int) error {
	members, pagination, err := api.readDirectory(kind, includeDisabled, maxPages)
	if err != nil {
		return err
	}

	return emitHandbook(map[string]interface{}{
		"data":       members,
		"pagination": pagination,
		"follow_up": map[string]interface{}{
			"member":       "wallfacer team get <id|name|handle|email>",
			"role_page":    "wallfacer handbook read <role-page-id>",
			"agent_tasks":  "wallfacer tasks list --created-by <agent-id>",
			"member_tasks": "wallfacer tasks list --owner-user-id <user-id>",
			"chat":         "wallfacer chat <agent> \"<what you need>\"",
			"run":          "wallfacer run <playbook> --message \"<kickoff>\"",
		},
	})
}

func runTeamGet(api *directoryAPI, reference string) error {
	member, err := api.resolveTeamMember(reference)
	if err != nil {
		return err
	}

	return emitHandbook(map[string]interface{}{
		"data":      member,
		"follow_up": member.FollowUp,
	})
}

// identifierField renders a record's ID as a string. Agent and user IDs arrive
// as JSON numbers; every command that consumes one takes it back as text, so
// they are normalized on the way in.
func identifierField(record map[string]interface{}, key string) string {
	switch value := record[key].(type) {
	case string:
		return value
	case float64:
		return strconv.FormatInt(int64(value), 10)
	}
	return ""
}

func boolField(record map[string]interface{}, key string) bool {
	value, _ := record[key].(bool)
	return value
}
