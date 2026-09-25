package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/spf13/viper"
)

const (
	agentJinID      = "101"
	agentAuggieID   = "102"
	agentSaulID     = "103"
	agentAdaID      = "104"
	agentPausedID   = "105"
	humanAdaID      = "201"
	humanGraceID    = "202"
	unknownMemberID = "999"

	playbookDraftOnlyID = "bbbbbbb5-1111-4111-8111-111111111111"

	chatTaskID = "f1f1f1f1-1111-4111-8111-111111111111"
	runTaskID  = "f2f2f2f2-1111-4111-8111-111111111111"
)

// teamFixture stands up the directory and the task endpoint, and delegates the
// handbook routes to the handbook fixture's own routing so `run` resolves
// playbook references exactly as `handbook` does.
//
// The directory holds the shapes that make picking a teammate hard: an agent
// and a human sharing a display name, an agent that only appears on the second
// page of results, an offboarded agent, a paused one, and a playbook with a
// draft and nothing published.
type teamFixture struct {
	server   *httptest.Server
	handbook handbookFixture

	mu       sync.Mutex
	requests []recordedRequest
	bodies   []map[string]interface{}

	// taskRefusal, when set, is the body the task endpoint answers a create
	// with, as a 422. The server-side guards behind those refusals are not
	// reachable from the directory fixtures.
	taskRefusal string
}

func newTeamFixture(t *testing.T) *teamFixture {
	t.Helper()

	fixture := &teamFixture{}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded := recordedRequest{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery}

		fixture.mu.Lock()
		fixture.requests = append(fixture.requests, recorded)
		if r.Body != nil && r.Method == http.MethodPost {
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				fixture.bodies = append(fixture.bodies, body)
			}
		}
		fixture.mu.Unlock()

		body, status := fixture.route(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(fixture.server.Close)

	viper.Set("server", fixture.server.URL)
	t.Cleanup(func() { viper.Set("server", "") })

	return fixture
}

func (f *teamFixture) route(r *http.Request) (string, int) {
	base := "/v1/accounts/" + testAccountID

	switch r.URL.Path {
	// The agents listing is cursor-paginated, as the API is: it answers with
	// the second page only for the cursor it handed out, and ignores a page=N
	// exactly the way cursorPaginate does.
	case base + "/agents":
		includeDisabled := r.URL.Query().Get("include_disabled") == "true"
		if r.URL.Query().Get("cursor") == agentsNextCursor {
			if includeDisabled {
				return agentsPageTwoWithDisabledJSON, http.StatusOK
			}
			return agentsPageTwoJSON, http.StatusOK
		}
		return agentsPageOneJSON, http.StatusOK

	case base + "/users":
		if r.URL.Query().Get("page") == "2" {
			return usersPageTwoJSON, http.StatusOK
		}
		return usersPageOneJSON, http.StatusOK

	case base + "/pipelines/" + playbookDraftOnlyID:
		return wrapData(playbookDraftOnlyRecordJSON), http.StatusOK

	case base + "/pipelines/" + playbookDisabledID:
		return wrapData(playbookDisabledRecordJSON), http.StatusOK

	case base + "/tasks":
		if r.Method != http.MethodPost {
			return emptyListJSON, http.StatusOK
		}
		if f.taskRefusal != "" {
			return f.taskRefusal, http.StatusUnprocessableEntity
		}
		return f.createdTask(), http.StatusCreated
	}

	return f.handbook.route(r)
}

// createdTask answers the task endpoint with a record shaped like the one the
// mode being exercised produces, so a test can check that the identifiers a
// later read needs survive the response.
func (f *teamFixture) createdTask() string {
	body := f.lastBody()
	if body["pipeline_id"] != nil {
		return wrapData(runTaskRecordJSON)
	}
	return wrapData(chatTaskRecordJSON)
}

func (f *teamFixture) recorded() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedRequest(nil), f.requests...)
}

func (f *teamFixture) lastBody() map[string]interface{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) == 0 {
		return map[string]interface{}{}
	}
	return f.bodies[len(f.bodies)-1]
}

func (f *teamFixture) api() *directoryAPI {
	return &directoryAPI{handbookAPI: &handbookAPI{accountID: testAccountID}}
}

// expectError runs a command body that is supposed to fail and returns the
// message, so a test can assert on what the caller is actually told.
func expectError(t *testing.T, run func() error) string {
	t.Helper()
	err := run()
	if err == nil {
		t.Fatal("expected the command to fail")
	}
	return err.Error()
}

// captureRaw runs a command body and returns exactly what it printed, for the
// cases where the output is projected down to something that is not an object.
func captureRaw(t *testing.T, run func() error) string {
	t.Helper()

	var buf bytes.Buffer
	previous := cli.Stdout
	cli.Stdout = &buf
	defer func() { cli.Stdout = previous }()

	if err := run(); err != nil {
		t.Fatalf("command failed: %v", err)
	}
	return buf.String()
}

func members(t *testing.T, output map[string]interface{}) []map[string]interface{} {
	t.Helper()
	items, ok := output["data"].([]interface{})
	if !ok {
		t.Fatalf("output has no data list: %v", output)
	}
	var records []map[string]interface{}
	for _, item := range items {
		record, ok := item.(map[string]interface{})
		if !ok {
			t.Fatalf("data item is not an object: %v", item)
		}
		records = append(records, record)
	}
	return records
}

func memberByID(t *testing.T, output map[string]interface{}, id string) map[string]interface{} {
	t.Helper()
	for _, record := range members(t, output) {
		if record["id"] == id {
			return record
		}
	}
	t.Fatalf("no member %s in %v", id, output["data"])
	return nil
}

func TestTeamListTraversesBothListingsBeyondTheFirstPage(t *testing.T) {
	fixture := newTeamFixture(t)

	output := capture(t, func() error { return runTeamList(fixture.api(), "", false, 20) })

	ids := map[string]bool{}
	for _, record := range members(t, output) {
		ids[record["id"].(string)] = true
	}

	// One agent and one human each live only on the second page.
	for _, id := range []string{agentJinID, agentAuggieID, agentAdaID, humanAdaID, humanGraceID} {
		if !ids[id] {
			t.Errorf("member %s missing; the sweep stopped short of the second page", id)
		}
	}

	pagination, ok := output["pagination"].(map[string]interface{})
	if !ok {
		t.Fatalf("no pagination in %v", output)
	}
	for _, listing := range []string{"agents", "humans"} {
		sweep, ok := pagination[listing].(map[string]interface{})
		if !ok {
			t.Fatalf("no %s sweep report: %v", listing, pagination)
		}
		if sweep["complete"] != true {
			t.Errorf("%s sweep reported incomplete: %v", listing, sweep)
		}
		if sweep["pages_read"].(float64) < 2 {
			t.Errorf("%s sweep read %v pages, want both", listing, sweep["pages_read"])
		}
	}
}

// The agents listing is cursor-paginated. Paged with page=N it would answer
// with the first page for every request, so the sweep would repeat those
// records and never see a null next, and an unbounded sweep (`team get`,
// `chat`, `run --agent`) would not terminate.
func TestTeamListPagesTheAgentsListingByCursor(t *testing.T) {
	fixture := newTeamFixture(t)

	output := capture(t, func() error { return runTeamList(fixture.api(), memberAgent, false, 20) })

	agentRequests := []recordedRequest{}
	for _, request := range fixture.recorded() {
		if strings.HasSuffix(request.path, "/agents") {
			agentRequests = append(agentRequests, request)
		}
	}
	if len(agentRequests) != 2 {
		t.Fatalf("the agents listing was read %d times, want both pages exactly once: %v", len(agentRequests), agentRequests)
	}
	if strings.Contains(agentRequests[0].query, "cursor=") {
		t.Errorf("the first request carried a cursor: %q", agentRequests[0].query)
	}
	if !strings.Contains(agentRequests[1].query, "cursor="+agentsNextCursor) {
		t.Errorf("the second request did not carry the cursor the first page reported: %q", agentRequests[1].query)
	}
	for _, request := range agentRequests {
		for _, param := range strings.Split(request.query, "&") {
			if strings.HasPrefix(param, "page=") {
				t.Errorf("the agents listing was paged with page=N: %q", request.query)
			}
		}
	}

	// Each agent exactly once, and the sweep knows it reached the end.
	seen := map[string]int{}
	for _, record := range members(t, output) {
		seen[record["id"].(string)]++
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("agent %s returned %d times", id, count)
		}
	}
	for _, id := range []string{agentJinID, agentAuggieID, agentAdaID} {
		if seen[id] != 1 {
			t.Errorf("agent %s missing from the sweep", id)
		}
	}
	sweep := output["pagination"].(map[string]interface{})["agents"].(map[string]interface{})
	if sweep["complete"] != true {
		t.Errorf("the agents sweep reported incomplete: %v", sweep)
	}
}

func TestTeamListCarriesTypeIdentityAndRoleData(t *testing.T) {
	fixture := newTeamFixture(t)

	output := capture(t, func() error { return runTeamList(fixture.api(), "", false, 20) })

	agent := memberByID(t, output, agentJinID)
	if agent["type"] != memberAgent {
		t.Errorf("agent record has type %v", agent["type"])
	}
	if agent["handle"] != "jin" || agent["name"] != "Jin" || agent["title"] != "Software Engineer" {
		t.Errorf("agent identity is incomplete: %v", agent)
	}
	if agent["role_page_id"] != pageEngineeringID {
		t.Errorf("agent record lost its role page: %v", agent)
	}
	if agent["chatable"] != true || agent["state"] != "active" {
		t.Errorf("active agent should be chatable: %v", agent)
	}

	human := memberByID(t, output, humanAdaID)
	if human["type"] != memberHuman {
		t.Errorf("human record has type %v", human["type"])
	}
	if human["role"] != "owner" || human["email"] != "ada@example.test" {
		t.Errorf("human identity is incomplete: %v", human)
	}
	if human["chatable"] != false {
		t.Errorf("a human is never a chat identity: %v", human)
	}
}

// github_username is documented as carried by every record, so it is present
// on all of them: the linked account as a string, and no linked account as
// null. Dropping the key would make "not linked" indistinguishable from a CLI
// that does not report the field, and `-q 'data[].github_username'` return
// nothing at all.
func TestTeamListCarriesGithubUsernamePresentOrNull(t *testing.T) {
	fixture := newTeamFixture(t)

	output := capture(t, func() error { return runTeamList(fixture.api(), "", false, 20) })

	for _, record := range members(t, output) {
		if _, present := record["github_username"]; !present {
			t.Errorf("member %v does not carry github_username: %v", record["id"], record)
		}
	}

	if got := memberByID(t, output, agentJinID)["github_username"]; got != "wallfacer-jin" {
		t.Errorf("linked agent reports github_username %v", got)
	}
	if got := memberByID(t, output, humanAdaID)["github_username"]; got != "ada" {
		t.Errorf("linked human reports github_username %v", got)
	}
	if got := memberByID(t, output, agentAdaID)["github_username"]; got != nil {
		t.Errorf("agent with no linked GitHub account reports github_username %v, want null", got)
	}
}

// The directory does not aggregate anybody's work. What it owes instead is the
// commands that do: a role-page read and the task list's own filters, with the
// IDs already filled in.
func TestTeamOutputPointsAtRolePageReadsAndTaskFilters(t *testing.T) {
	fixture := newTeamFixture(t)

	output := capture(t, func() error { return runTeamList(fixture.api(), "", false, 20) })

	agent := memberByID(t, output, agentJinID)
	followUp, ok := agent["follow_up"].(map[string]interface{})
	if !ok {
		t.Fatalf("agent record has no follow_up: %v", agent)
	}
	if want := "wallfacer handbook read " + pageEngineeringID; followUp["role_page"] != want {
		t.Errorf("role_page is %v, want %q", followUp["role_page"], want)
	}
	if want := "wallfacer tasks list --created-by " + agentJinID; followUp["tasks"] != want {
		t.Errorf("agent tasks filter is %v, want %q", followUp["tasks"], want)
	}

	human := memberByID(t, output, humanAdaID)
	humanFollowUp := human["follow_up"].(map[string]interface{})
	if want := "wallfacer tasks list --owner-user-id " + humanAdaID; humanFollowUp["tasks"] != want {
		t.Errorf("human tasks filter is %v, want %q", humanFollowUp["tasks"], want)
	}

	// The role page the directory named is readable with the command it named.
	page := capture(t, func() error { return runHandbookRead(fixture.api().handbookAPI, pageEngineeringID, "") })
	if record := page["data"].(map[string]interface{}); record["id"] != pageEngineeringID {
		t.Errorf("following the role page reference read %v", record["id"])
	}
}

func TestTeamListOmitsOffboardedAgentsUnlessAsked(t *testing.T) {
	fixture := newTeamFixture(t)

	plain := capture(t, func() error { return runTeamList(fixture.api(), memberAgent, false, 20) })
	for _, record := range members(t, plain) {
		if record["id"] == agentSaulID {
			t.Fatal("a disabled agent should stay out of the plain listing")
		}
	}

	withDisabled := capture(t, func() error { return runTeamList(fixture.api(), memberAgent, true, 20) })
	offboarded := memberByID(t, withDisabled, agentSaulID)
	if offboarded["state"] != "disabled" || offboarded["chatable"] != false {
		t.Errorf("offboarded agent is reported as %v", offboarded)
	}
}

func TestTeamGetResolvesIDHandleEmailAndName(t *testing.T) {
	fixture := newTeamFixture(t)

	cases := []struct {
		reference string
		wantID    string
		wantFrom  string
	}{
		{agentJinID, agentJinID, "id"},
		{"jin", agentJinID, "handle"},
		{"@auggie", agentAuggieID, "handle"},
		{"grace@example.test", humanGraceID, "email"},
		{"Grace Hopper", humanGraceID, "name"},
		{"jIn", agentJinID, "handle"},
	}

	for _, test := range cases {
		output := capture(t, func() error { return runTeamGet(fixture.api(), test.reference) })
		record := output["data"].(map[string]interface{})
		if record["id"] != test.wantID {
			t.Errorf("%q resolved to %v, want %s", test.reference, record["id"], test.wantID)
		}
		if record["resolved_from"] != test.wantFrom {
			t.Errorf("%q resolved from %v, want %s", test.reference, record["resolved_from"], test.wantFrom)
		}
	}
}

func TestTeamGetReportsDuplicateNamesRatherThanGuessing(t *testing.T) {
	fixture := newTeamFixture(t)

	message := expectError(t, func() error { return runTeamGet(fixture.api(), "Ada Lovelace") })

	for _, want := range []string{"matches 2", agentAdaID, humanAdaID} {
		if !strings.Contains(message, want) {
			t.Errorf("ambiguous name error %q does not report %q", message, want)
		}
	}
}

func TestTeamGetFailsClearlyOnAMissingMember(t *testing.T) {
	fixture := newTeamFixture(t)

	message := expectError(t, func() error { return runTeamGet(fixture.api(), unknownMemberID) })
	if !strings.Contains(message, "no agent or member") || !strings.Contains(message, unknownMemberID) {
		t.Errorf("missing member error is %q", message)
	}
}

func TestChatSendsThePromptContractAndNamesTheAgent(t *testing.T) {
	fixture := newTeamFixture(t)

	output := capture(t, func() error {
		return runChat(fixture.api(), "jin", "Look at the failing build", taskOptions{title: "Failing build"})
	})

	body := fixture.lastBody()
	if body["prompt"] != "Look at the failing build" {
		t.Errorf("chat did not send the freeform prompt: %v", body)
	}
	if body["created_by"] != float64(101) {
		t.Errorf("chat did not name the selected agent: %v", body)
	}
	if _, present := body["pipeline_id"]; present {
		t.Errorf("chat must never fall back to a playbook run: %v", body)
	}
	if _, present := body["message"]; present {
		t.Errorf("message belongs to a playbook run, not to chat: %v", body)
	}
	if body["title"] != "Failing build" {
		t.Errorf("chat dropped the title: %v", body)
	}

	if output["mode"] != "chat" {
		t.Errorf("chat reported mode %v", output["mode"])
	}
	agent := output["agent"].(map[string]interface{})
	if agent["id"] != agentJinID || agent["handle"] != "jin" {
		t.Errorf("chat output does not name the agent it selected: %v", agent)
	}
}

func TestRunSendsThePipelineContractAndClaimsNoActorOverride(t *testing.T) {
	fixture := newTeamFixture(t)

	output := capture(t, func() error {
		return runPlaybook(fixture.api(), "Build", "Start with the checkout regression", "", taskOptions{})
	})

	body := fixture.lastBody()
	if body["pipeline_id"] != playbookBuildID {
		t.Errorf("run did not send the playbook: %v", body)
	}
	if body["message"] != "Start with the checkout regression" {
		t.Errorf("run did not send the kickoff message: %v", body)
	}
	if _, present := body["prompt"]; present {
		t.Errorf("run must never fall back to a freeform prompt: %v", body)
	}
	// The server runs the version it has active; the CLI never pins one.
	for _, field := range []string{"pipeline_version", "pipeline_version_id"} {
		if _, present := body[field]; present {
			t.Errorf("run pinned %s instead of using the active published version: %v", field, body)
		}
	}

	if output["mode"] != "playbook" {
		t.Errorf("run reported mode %v", output["mode"])
	}
	actors, _ := output["step_actors"].(string)
	if !strings.Contains(actors, "published version") {
		t.Errorf("run should say its step actors come from the published version, got %q", actors)
	}
}

func TestRunAgentIsTaskIdentityNotAStepActorOverride(t *testing.T) {
	fixture := newTeamFixture(t)

	output := capture(t, func() error {
		return runPlaybook(fixture.api(), playbookBuildID, "", "jin", taskOptions{})
	})

	body := fixture.lastBody()
	if body["created_by"] != float64(101) {
		t.Errorf("run did not set the task identity: %v", body)
	}
	if _, present := body["prompt"]; present {
		t.Errorf("naming an agent must not turn a run into a chat: %v", body)
	}
	if actors, _ := output["step_actors"].(string); strings.Contains(actors, "override") {
		t.Errorf("run claimed to override step actors: %q", actors)
	}
	if agent := output["agent"].(map[string]interface{}); agent["id"] != agentJinID {
		t.Errorf("run output does not name the identity it used: %v", agent)
	}
}

func TestChatRefusesAHumanAndAnUnavailableAgent(t *testing.T) {
	fixture := newTeamFixture(t)

	human := expectError(t, func() error { return runChat(fixture.api(), "grace@example.test", "hello", taskOptions{}) })
	if !strings.Contains(human, "human member") {
		t.Errorf("chat to a human failed with %q", human)
	}

	disabled := expectError(t, func() error { return runChat(fixture.api(), "saul", "hello", taskOptions{}) })
	if !strings.Contains(disabled, "disabled") {
		t.Errorf("chat to an offboarded agent failed with %q", disabled)
	}

	paused := expectError(t, func() error { return runChat(fixture.api(), "rester", "hello", taskOptions{}) })
	if !strings.Contains(paused, "paused") {
		t.Errorf("chat to a paused agent failed with %q", paused)
	}

	missing := expectError(t, func() error { return runChat(fixture.api(), "nobody", "hello", taskOptions{}) })
	if !strings.Contains(missing, "no agent or member") {
		t.Errorf("chat to a missing agent failed with %q", missing)
	}

	for _, request := range fixture.recorded() {
		if request.method == http.MethodPost {
			t.Errorf("a refused chat still created something: %s %s", request.method, request.path)
		}
	}
}

func TestRunRefusesPagesDraftsAndOtherAccounts(t *testing.T) {
	fixture := newTeamFixture(t)

	page := expectError(t, func() error { return runPlaybook(fixture.api(), pageBuildID, "", "", taskOptions{}) })
	if !strings.Contains(page, "is a page") {
		t.Errorf("running a page failed with %q", page)
	}

	draft := expectError(t, func() error { return runPlaybook(fixture.api(), playbookDraftOnlyID, "", "", taskOptions{}) })
	if !strings.Contains(draft, "is draft-only") || !strings.Contains(draft, "wallfacer handbook publish") {
		t.Errorf("running a draft-only playbook failed with %q", draft)
	}

	archived := expectError(t, func() error { return runPlaybook(fixture.api(), playbookArchivedID, "", "", taskOptions{}) })
	if !strings.Contains(archived, "archived") {
		t.Errorf("running an archived playbook failed with %q", archived)
	}

	foreign := "https://app.wallfacer.ai/accounts/" + otherAccountID + "/handbook/" + playbookBuildID
	other := expectError(t, func() error { return runPlaybook(fixture.api(), foreign, "", "", taskOptions{}) })
	if !strings.Contains(other, "belongs to account "+otherAccountID) {
		t.Errorf("a reference from another account failed with %q", other)
	}

	ambiguous := expectError(t, func() error { return runPlaybook(fixture.api(), unknownID, "", "", taskOptions{}) })
	if !strings.Contains(ambiguous, "no playbook found") {
		t.Errorf("running a missing playbook failed with %q", ambiguous)
	}

	for _, request := range fixture.recorded() {
		if request.method == http.MethodPost {
			t.Errorf("a refused run still created something: %s %s", request.method, request.path)
		}
	}
}

// The environment guard lives on the server: it refuses a run whose playbook
// has a step the performing agent has no computer for. The CLI's other run
// refusals name the command that clears them, and this one reads the same.
func TestRunPhrasesTheEnvironmentRefusal(t *testing.T) {
	fixture := newTeamFixture(t)
	fixture.taskRefusal = environmentRequiredJSON

	message := expectError(t, func() error {
		return runPlaybook(fixture.api(), playbookBuildID, "", "", taskOptions{})
	})

	for _, want := range []string{"needs an environment", playbookBuildID, "--environment-id"} {
		if !strings.Contains(message, want) {
			t.Errorf("the environment refusal %q does not report %q", message, want)
		}
	}
	if strings.Contains(message, "HTTP 422") || strings.Contains(message, "\"code\"") {
		t.Errorf("the environment refusal dumped the API's error body: %q", message)
	}
}

// Every other rejected create is still the API's own error text: only the
// refusals the CLI has words for are rephrased.
func TestRunKeepsUnrecognizedRefusalsRaw(t *testing.T) {
	fixture := newTeamFixture(t)
	fixture.taskRefusal = `{"message":"The given data was invalid.","errors":{"title":["The title field is required."]}}`

	message := expectError(t, func() error {
		return runPlaybook(fixture.api(), playbookBuildID, "", "", taskOptions{})
	})

	if !strings.Contains(message, "HTTP 422") || !strings.Contains(message, "The title field is required.") {
		t.Errorf("an unrecognized refusal was not passed through: %q", message)
	}
}

// Disabling a playbook clears its triggers; it does not take it out of service.
// The API accepts a manual run of one and the app offers it, so the CLI sends
// the same request it sends for an enabled playbook.
func TestRunFiresADisabledPlaybook(t *testing.T) {
	fixture := newTeamFixture(t)

	output := capture(t, func() error {
		return runPlaybook(fixture.api(), playbookDisabledID, "Fire it by hand", "", taskOptions{})
	})

	body := fixture.lastBody()
	if body["pipeline_id"] != playbookDisabledID {
		t.Errorf("a disabled playbook did not reach the task endpoint: %v", body)
	}
	if body["message"] != "Fire it by hand" {
		t.Errorf("run did not send the kickoff message: %v", body)
	}

	playbook := output["playbook"].(map[string]interface{})
	if playbook["state"] != "disabled" {
		t.Errorf("run should report the state it fired, got %v", playbook["state"])
	}
	if note, _ := output["disabled_note"].(string); !strings.Contains(note, "triggers") {
		t.Errorf("run should note the cleared triggers, got %q", note)
	}
}

func TestRunAcceptsAPlaybookDetailURL(t *testing.T) {
	fixture := newTeamFixture(t)

	capture(t, func() error {
		reference := "https://app.wallfacer.ai/accounts/" + testAccountID + "/handbook/" + playbookBuildID
		return runPlaybook(fixture.api(), reference, "", "", taskOptions{})
	})

	if body := fixture.lastBody(); body["pipeline_id"] != playbookBuildID {
		t.Errorf("a detail URL did not resolve to the playbook: %v", body)
	}
}

func TestResponsesPreserveIdentifiersForTheNextRead(t *testing.T) {
	fixture := newTeamFixture(t)

	chat := capture(t, func() error { return runChat(fixture.api(), "jin", "hello", taskOptions{}) })
	chatTask := chat["data"].(map[string]interface{})
	if chatTask["id"] != chatTaskID {
		t.Errorf("chat response lost the task ID: %v", chatTask)
	}
	chatFollowUp := chat["follow_up"].(map[string]interface{})
	if want := "wallfacer tasks get " + chatTaskID; chatFollowUp["task"] != want {
		t.Errorf("chat follow_up task is %v, want %q", chatFollowUp["task"], want)
	}
	if want := "wallfacer sessions list " + chatTaskID; chatFollowUp["sessions"] != want {
		t.Errorf("chat follow_up sessions is %v, want %q", chatFollowUp["sessions"], want)
	}
	if _, present := chatFollowUp["version"]; present {
		t.Error("a chat task has no playbook version to read")
	}

	run := capture(t, func() error { return runPlaybook(fixture.api(), playbookBuildID, "", "", taskOptions{}) })
	runTask := run["data"].(map[string]interface{})
	if runTask["pipeline_version_id"] != playbookVersionID {
		t.Errorf("run response lost the version ID: %v", runTask)
	}
	runFollowUp := run["follow_up"].(map[string]interface{})
	if want := "wallfacer handbook version " + playbookBuildID + " 2"; runFollowUp["version"] != want {
		t.Errorf("run follow_up version is %v, want %q", runFollowUp["version"], want)
	}
	if want := "wallfacer sessions list " + runTaskID; runFollowUp["sessions"] != want {
		t.Errorf("run follow_up sessions is %v, want %q", runFollowUp["sessions"], want)
	}
}

func TestRejectedTaskCreateSurfacesTheAPIError(t *testing.T) {
	fixture := newTeamFixture(t)

	// Point the client at a server that refuses the create the way the API
	// does, so the caller sees the API's own reason rather than a generic one.
	refusing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tasks") && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, `{"errors":[{"message":"The selected agent is not enabled.","code":"validation_failed"}]}`)
			return
		}
		body, status := fixture.route(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	defer refusing.Close()
	viper.Set("server", refusing.URL)

	message := expectError(t, func() error { return runChat(fixture.api(), "jin", "hello", taskOptions{}) })
	if !strings.Contains(message, "422") || !strings.Contains(message, "The selected agent is not enabled.") {
		t.Errorf("a rejected create reported %q", message)
	}
}

func TestTeamLookupNeverCreatesATask(t *testing.T) {
	fixture := newTeamFixture(t)

	capture(t, func() error { return runTeamList(fixture.api(), "", true, 20) })
	capture(t, func() error { return runTeamGet(fixture.api(), "jin") })

	recorded := fixture.recorded()
	if len(recorded) == 0 {
		t.Fatal("expected the directory commands to issue requests")
	}
	for _, request := range recorded {
		if request.method != http.MethodGet {
			t.Errorf("%s %s: team lookup must never be anything but a read", request.method, request.path)
		}
		if strings.Contains(request.path, "/tasks") {
			t.Errorf("team lookup must not touch tasks: %s", request.path)
		}
	}
}

// Running a playbook consumes it. It never edits the playbook and never
// publishes the draft sitting next to it.
func TestRunningAPlaybookOnlyReadsIt(t *testing.T) {
	fixture := newTeamFixture(t)

	capture(t, func() error { return runPlaybook(fixture.api(), playbookBuildID, "go", "", taskOptions{}) })

	for _, request := range fixture.recorded() {
		if strings.Contains(request.path, "/pipelines") && request.method != http.MethodGet {
			t.Errorf("%s %s: a run must not write to the playbook", request.method, request.path)
		}
		if strings.Contains(request.path, "/pages") && request.method != http.MethodGet {
			t.Errorf("%s %s: a run must not write to a page", request.method, request.path)
		}
	}
}

func TestTeamOutputStaysProjectableWithQuery(t *testing.T) {
	fixture := newTeamFixture(t)

	viper.Set("query", "data.id")
	defer viper.Set("query", "")

	output := captureRaw(t, func() error { return runTeamGet(fixture.api(), "jin") })
	if got := strings.TrimSpace(output); got != `"`+agentJinID+`"` {
		t.Errorf("query projection returned %s, want the agent ID", got)
	}
}

// The task endpoint's own refusal for a playbook step whose performer has no
// computer, copied from the API.
const environmentRequiredJSON = `{"errors":[{"message":"This handbook has a step that needs a computer before it can be run.","code":"environment_required"}]}`

// The cursors the agents fixture hands out, opaque as the API's own are.
const (
	agentsNextCursor = "eyJ1c2VyX2lkIjoxMDUsIl9wb2ludHNUb05leHRJdGVtcyI6dHJ1ZX0"
	agentsPrevCursor = "eyJ1c2VyX2lkIjoxMDIsIl9wb2ludHNUb05leHRJdGVtcyI6ZmFsc2V9"
)

const agentJinRecordJSON = `{"id":101,"runtime":{"status":"ready","source":"account","vendor":"claude"},"display_name":"Jin","handle":"jin","email":"jin@example.test","title":"Software Engineer","role_page_id":"` + pageEngineeringID + `","environment_id":"eeee1111-1111-4111-8111-111111111111","vendor":"claude","model":"claude-opus-5","disabled":false,"disabled_at":null,"paused":false,"paused_at":null,"github_connected":true,"github_username":"wallfacer-jin","created_at":"2026-08-01T00:00:00.000000Z"}`

const agentsPageOneJSON = `{"data":[
  ` + agentJinRecordJSON + `,
  {"id":105,"runtime":{"status":"ready"},"display_name":"Rester","handle":"rester","email":"rester@example.test","title":"Release Tester","role_page_id":null,"environment_id":null,"disabled":false,"paused":true,"paused_at":"2026-09-10T00:00:00.000000Z","github_username":null}
],"links":{"first":null,"last":null,"prev":null,"next":"http://example.test/agents?cursor=` + agentsNextCursor + `"},"meta":{"path":"http://example.test/agents","per_page":2,"next_cursor":"` + agentsNextCursor + `","prev_cursor":null}}`

const agentsPageTwoJSON = `{"data":[
  {"id":102,"runtime":{"status":"ready"},"display_name":"Auggie","handle":"auggie","email":"auggie@example.test","title":"Code Reviewer","role_page_id":"` + pageReviewEngID + `","environment_id":null,"disabled":false,"paused":false,"github_username":"wallfacer-auggie"},
  {"id":104,"runtime":{"status":"ready"},"display_name":"Ada Lovelace","handle":"ada-agent","email":"ada-agent@example.test","title":"Research Agent","role_page_id":null,"environment_id":null,"disabled":false,"paused":false,"github_username":null}
],"links":{"first":null,"last":null,"prev":"http://example.test/agents?cursor=` + agentsPrevCursor + `","next":null},"meta":{"path":"http://example.test/agents","per_page":2,"next_cursor":null,"prev_cursor":"` + agentsPrevCursor + `"}}`

// The disabled agent is only ever returned with include_disabled=true, exactly
// as the API behaves.
const agentsPageTwoWithDisabledJSON = `{"data":[
  {"id":102,"runtime":{"status":"ready"},"display_name":"Auggie","handle":"auggie","email":"auggie@example.test","title":"Code Reviewer","role_page_id":"` + pageReviewEngID + `","environment_id":null,"disabled":false,"paused":false,"github_username":"wallfacer-auggie"},
  {"id":104,"runtime":{"status":"ready"},"display_name":"Ada Lovelace","handle":"ada-agent","email":"ada-agent@example.test","title":"Research Agent","role_page_id":null,"environment_id":null,"disabled":false,"paused":false,"github_username":null},
  {"id":103,"runtime":{"status":"needs_credentials"},"display_name":"Saul","handle":"saul","email":"saul@example.test","title":"Support Engineer","role_page_id":null,"environment_id":null,"disabled":true,"disabled_at":"2026-09-01T00:00:00.000000Z","paused":false,"github_username":null}
],"links":{"first":null,"last":null,"prev":"http://example.test/agents?cursor=` + agentsPrevCursor + `","next":null},"meta":{"path":"http://example.test/agents","per_page":2,"next_cursor":null,"prev_cursor":"` + agentsPrevCursor + `"}}`

const usersPageOneJSON = `{"data":[
  {"id":201,"name":"Ada Lovelace","email":"ada@example.test","avatar_url":null,"github_username":"ada","role":"owner","joined_at":"2026-01-01T00:00:00.000000Z","removed_at":null}
],"links":{"first":"http://example.test/users?page=1","last":"http://example.test/users?page=2","prev":null,"next":"http://example.test/users?page=2"},"meta":{"current_page":1,"last_page":2,"per_page":1,"total":2}}`

const usersPageTwoJSON = `{"data":[
  {"id":202,"name":"Grace Hopper","email":"grace@example.test","avatar_url":null,"github_username":"grace","role":"member","joined_at":"2026-02-01T00:00:00.000000Z","removed_at":null}
],"links":{"first":"http://example.test/users?page=1","last":"http://example.test/users?page=2","prev":"http://example.test/users?page=1","next":null},"meta":{"current_page":2,"last_page":2,"per_page":1,"total":2}}`

const playbookDraftOnlyRecordJSON = `{"id":"` + playbookDraftOnlyID + `","account_id":"` + testAccountID + `","name":"Unpublished Playbook","description":"Never published.","active_version":null,"version_count":0,"draft":{"definition":{"steps":[{"id":"first","kind":"ai","title":"First cut"}]},"updated_at":"2026-09-12T00:00:00.000000Z","updated_by":1},"parent_page_id":null,"position":4,"linked_page_ids":[],"disabled_at":null,"archived_at":null,"created_at":"2026-09-11T00:00:00.000000Z","created_by":1}`

const chatTaskRecordJSON = `{"id":"` + chatTaskID + `","account_id":"` + testAccountID + `","environment_id":"eeee1111-1111-4111-8111-111111111111","title":"Failing build","prompt":"Look at the failing build","status":"active","pipeline_id":null,"pipeline_version_id":null,"pipeline_version":null,"origin":{"kind":"manual"},"owner_user_id":201,"performer_user_id":null,"created_by":101,"created_at":"2026-09-18T00:00:00.000000Z"}`

const runTaskRecordJSON = `{"id":"` + runTaskID + `","account_id":"` + testAccountID + `","environment_id":"eeee1111-1111-4111-8111-111111111111","title":"Build","prompt":null,"status":"active","pipeline_id":"` + playbookBuildID + `","pipeline_version_id":"` + playbookVersionID + `","pipeline_version":2,"current_step_id":"implement","current_step_index":0,"steps_total":1,"origin":{"kind":"playbook","playbook_name":"Build"},"owner_user_id":201,"performer_user_id":null,"created_by":101,"created_at":"2026-09-18T00:00:00.000000Z"}`

func TestTheHumanRefusalNamesTheMemberOnce(t *testing.T) {
	fixture := newTeamFixture(t)

	byName := expectError(t, func() error { return runChat(fixture.api(), "Grace Hopper", "hello", taskOptions{}) })
	if strings.Count(byName, "Grace Hopper") != 1 {
		t.Errorf("the refusal repeats the name: %q", byName)
	}

	byEmail := expectError(t, func() error { return runChat(fixture.api(), "grace@example.test", "hello", taskOptions{}) })
	if !strings.Contains(byEmail, "Grace Hopper") || !strings.Contains(byEmail, "human member") {
		t.Errorf("a reference by email should still name the member: %q", byEmail)
	}
}

func TestRunResolvesTheAgentBeforeThePlaybook(t *testing.T) {
	fixture := newTeamFixture(t)

	message := expectError(t, func() error { return runPlaybook(fixture.api(), playbookBuildID, "", "nobody", taskOptions{}) })
	if !strings.Contains(message, "no agent or member") {
		t.Errorf("an unresolvable agent failed with %q", message)
	}

	for _, request := range fixture.recorded() {
		if strings.Contains(request.path, "/pipelines") || strings.Contains(request.path, "/pages") {
			t.Errorf("the playbook was read though --agent could not resolve: %s %s", request.method, request.path)
		}
	}
}

// Resolving --agent before the playbook must not cost the reference's local
// guard its place: a playbook URL from another account is refused with no
// request at all, however usable --agent is.
func TestRunRefusesAnotherAccountsPlaybookBeforeResolvingTheAgent(t *testing.T) {
	fixture := newTeamFixture(t)

	foreign := "https://app.wallfacer.ai/accounts/" + otherAccountID + "/handbook/" + playbookBuildID
	message := expectError(t, func() error { return runPlaybook(fixture.api(), foreign, "", "jin", taskOptions{}) })
	if !strings.Contains(message, "belongs to account "+otherAccountID) {
		t.Errorf("a reference from another account failed with %q", message)
	}

	if got := fixture.recorded(); len(got) != 0 {
		t.Errorf("expected the mismatch to be caught before any request, got %d: %s %s", len(got), got[0].method, got[0].path)
	}
}
