package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/spf13/viper"
)

const (
	testAccountID  = "11111111-1111-4111-8111-111111111111"
	otherAccountID = "22222222-2222-4222-8222-222222222222"

	pageEngineeringID = "aaaaaaa1-1111-4111-8111-111111111111"
	pageBuildID       = "aaaaaaa2-1111-4111-8111-111111111111"
	pageReviewEngID   = "aaaaaaa3-1111-4111-8111-111111111111"
	pageProductID     = "aaaaaaa4-1111-4111-8111-111111111111"
	pageReviewProdID  = "aaaaaaa5-1111-4111-8111-111111111111"
	pageDeletedID     = "aaaaaaa9-1111-4111-8111-111111111111"
	pageCreatedID     = "aaaaaaa6-1111-4111-8111-111111111111"

	playbookBuildID    = "bbbbbbb1-1111-4111-8111-111111111111"
	playbookArchivedID = "bbbbbbb9-1111-4111-8111-111111111111"

	playbookVersionID      = "ccccccc1-1111-4111-8111-111111111111"
	playbookVersionFirstID = "ccccccc1-1111-4111-8111-111111111112"
	revisionID             = "ddddddd1-1111-4111-8111-111111111111"

	unknownID = "eeeeeee1-1111-4111-8111-111111111111"
)

func TestMain(m *testing.M) {
	cli.Init(&cli.Config{AppName: "wallfacer", EnvPrefix: "WALLFACER", Version: "test"})
	os.Exit(m.Run())
}

// recordedRequest is one call the CLI made, so a test can assert that a
// discovery command only ever reads, and that a write went to the intended
// resource with the intended body.
type recordedRequest struct {
	method string
	path   string
	query  string
	body   string
}

// decodedBody is the request's JSON body as a map, for asserting on the fields
// a write actually sent.
func (r recordedRequest) decodedBody(t *testing.T) map[string]interface{} {
	t.Helper()
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(r.body), &decoded); err != nil {
		t.Fatalf("%s %s: body is not a JSON object: %v\n%s", r.method, r.path, err, r.body)
	}
	return decoded
}

type handbookFixture struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []recordedRequest

	// writes, when set, answers a write request instead of the default
	// routing, so one test can make a page update fail the way the server
	// would (a parent cycle, an entry in another account) without the others
	// carrying that behaviour.
	writes func(r *http.Request, body string) (string, int, bool)
}

// newHandbookFixture stands up a handbook containing the shapes that make
// reference resolution hard: a page and a playbook sharing a title, the same
// title used under two different parents, entries that only appear on the
// second page of results, and a deleted page and an archived playbook that the
// tree does not list at all.
func newHandbookFixture(t *testing.T) *handbookFixture {
	t.Helper()

	fixture := &handbookFixture{}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent, _ := io.ReadAll(r.Body)

		fixture.mu.Lock()
		fixture.requests = append(fixture.requests, recordedRequest{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			body:   string(sent),
		})
		writes := fixture.writes
		fixture.mu.Unlock()

		if writes != nil && r.Method != http.MethodGet {
			if body, status, handled := writes(r, string(sent)); handled {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				fmt.Fprint(w, body)
				return
			}
		}

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

func (f *handbookFixture) route(r *http.Request) (string, int) {
	base := "/v1/accounts/" + testAccountID

	if r.Method != http.MethodGet {
		return f.routeWrite(r, base)
	}

	switch r.URL.Path {
	case base + "/handbook":
		return handbookTreeJSON, http.StatusOK

	case base + "/pages":
		if r.URL.Query().Get("page") == "2" {
			if r.URL.Query().Get("include_deleted") == "true" {
				return pagesPageTwoWithDeletedJSON, http.StatusOK
			}
			return pagesPageTwoJSON, http.StatusOK
		}
		return pagesPageOneJSON, http.StatusOK

	case base + "/pipelines":
		if r.URL.Query().Get("page") == "2" {
			return emptyListJSON, http.StatusOK
		}
		if r.URL.Query().Get("include_archived") == "true" {
			return pipelinesWithArchivedJSON, http.StatusOK
		}
		return pipelinesJSON, http.StatusOK

	case base + "/pages/" + pageBuildID:
		return wrapData(pageBuildRecordJSON), http.StatusOK
	case base + "/pages/" + pageBuildID + "/revisions":
		return revisionsJSON, http.StatusOK
	case base + "/pages/" + pageBuildID + "/revisions/" + revisionID:
		return wrapData(revisionRecordJSON), http.StatusOK

	case base + "/pipelines/" + playbookBuildID:
		return wrapData(playbookRecordJSON), http.StatusOK
	case base + "/pipelines/" + playbookArchivedID:
		return wrapData(playbookArchivedRecordJSON), http.StatusOK
	case base + "/pipelines/" + playbookBuildID + "/versions":
		if r.URL.Query().Get("page") == "2" {
			return playbookVersionsPageTwoJSON, http.StatusOK
		}
		return playbookVersionsJSON, http.StatusOK
	case base + "/pipelines/" + playbookBuildID + "/versions/2":
		return wrapData(playbookVersionRecordJSON), http.StatusOK
	}

	return `{"errors":[{"message":"Not found","code":"not_found"}]}`, http.StatusNotFound
}

// routeWrite answers the page, pipeline, and handbook writes. Each route
// returns the record the server would return for that write, so a test asserts
// on the request it recorded and on what the command did with the response.
func (f *handbookFixture) routeWrite(r *http.Request, base string) (string, int) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == base+"/pages":
		return wrapData(pageCreatedRecordJSON), http.StatusCreated

	case r.Method == http.MethodPatch && r.URL.Path == base+"/pages/"+pageBuildID:
		return wrapData(pageBuildRecordJSON), http.StatusOK

	case r.Method == http.MethodPatch && r.URL.Path == base+"/pages/"+pageDeletedID:
		return wrapData(pageRestoredRecordJSON), http.StatusOK

	case r.Method == http.MethodDelete && r.URL.Path == base+"/pages/"+pageEngineeringID:
		return "", http.StatusNoContent

	case r.Method == http.MethodDelete && r.URL.Path == base+"/pages/"+pageBuildID:
		return "", http.StatusNoContent

	case r.Method == http.MethodPatch && r.URL.Path == base+"/pipelines/"+playbookBuildID:
		return wrapData(playbookRecordJSON), http.StatusOK

	case r.Method == http.MethodPatch && r.URL.Path == base+"/pipelines/"+playbookArchivedID:
		return wrapData(playbookArchivedRecordJSON), http.StatusOK

	case r.Method == http.MethodPatch && r.URL.Path == base+"/handbook":
		return handbookTreeJSON, http.StatusOK
	}

	return `{"errors":[{"message":"Not found","code":"not_found"}]}`, http.StatusNotFound
}

func (f *handbookFixture) onWrite(fn func(r *http.Request, body string) (string, int, bool)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = fn
}

// mutations returns every non-GET request the commands made, which is how a
// test shows that a rejected reference never reached a write.
func (f *handbookFixture) mutations() []recordedRequest {
	var out []recordedRequest
	for _, request := range f.recorded() {
		if request.method != http.MethodGet {
			out = append(out, request)
		}
	}
	return out
}

func (f *handbookFixture) recorded() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedRequest(nil), f.requests...)
}

func (f *handbookFixture) api() *handbookAPI {
	return &handbookAPI{accountID: testAccountID}
}

// capture runs a command body and returns whatever it printed.
func capture(t *testing.T, run func() error) map[string]interface{} {
	t.Helper()

	var buf bytes.Buffer
	previous := cli.Stdout
	cli.Stdout = &buf
	defer func() { cli.Stdout = previous }()

	if err := run(); err != nil {
		t.Fatalf("command failed: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not a JSON object: %v\n%s", err, buf.String())
	}
	return decoded
}

func TestResolveAmbiguousNameReportsCandidates(t *testing.T) {
	fixture := newHandbookFixture(t)

	_, err := fixture.api().resolveHandbookRef("Review", "")
	if err == nil {
		t.Fatal("expected an ambiguity error")
	}

	ambiguous, ok := err.(*ambiguousRefError)
	if !ok {
		t.Fatalf("expected *ambiguousRefError, got %T: %v", err, err)
	}
	if len(ambiguous.candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(ambiguous.candidates))
	}
	for _, want := range []string{"Engineering/Review", "Product/Review"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not name candidate %q: %s", want, err.Error())
		}
	}
}

func TestResolveSharedTitleNeedsAType(t *testing.T) {
	fixture := newHandbookFixture(t)

	if _, err := fixture.api().resolveHandbookRef("Build", ""); err == nil {
		t.Fatal("expected a page and a playbook sharing a title to be ambiguous")
	}

	page, err := fixture.api().resolveHandbookRef("Build", kindPage)
	if err != nil {
		t.Fatalf("resolving the page: %v", err)
	}
	if page.ID != pageBuildID || page.Type != kindPage {
		t.Errorf("got %s %s, want page %s", page.Type, page.ID, pageBuildID)
	}

	playbook, err := fixture.api().resolveHandbookRef("Build", kindPlaybook)
	if err != nil {
		t.Fatalf("resolving the playbook: %v", err)
	}
	if playbook.ID != playbookBuildID || playbook.Type != kindPlaybook {
		t.Errorf("got %s %s, want playbook %s", playbook.Type, playbook.ID, playbookBuildID)
	}
}

func TestResolveByPathSeparatesDuplicateTitles(t *testing.T) {
	fixture := newHandbookFixture(t)

	cases := map[string]string{
		"Engineering/Review":  pageReviewEngID,
		"Product/Review":      pageReviewProdID,
		"engineering/review":  pageReviewEngID,
		"/Product/Review/":    pageReviewProdID,
		"Engineering/Product": "",
	}

	for reference, wantID := range cases {
		ref, err := fixture.api().resolveHandbookRef(reference, "")
		if wantID == "" {
			if err == nil {
				t.Errorf("%q: expected no match, got %s", reference, ref.ID)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", reference, err)
			continue
		}
		if ref.ID != wantID {
			t.Errorf("%q: got %s, want %s", reference, ref.ID, wantID)
		}
		if ref.ResolvedFrom != "path" {
			t.Errorf("%q: resolved_from is %q, want path", reference, ref.ResolvedFrom)
		}
	}
}

func TestResolveSupportedDetailURLs(t *testing.T) {
	fixture := newHandbookFixture(t)

	cases := []struct {
		reference string
		wantID    string
		wantKind  string
		wantFrom  string
	}{
		{"https://app.wallfacer.ai/accounts/" + testAccountID + "/handbook/pages/" + pageBuildID, pageBuildID, kindPage, "url"},
		{"https://app.wallfacer.ai/accounts/" + testAccountID + "/handbook/" + playbookBuildID, playbookBuildID, kindPlaybook, "url"},
		{"https://app.wallfacer.ai/accounts/" + testAccountID + "/handbook/" + playbookBuildID + "/versions/2", playbookBuildID, kindPlaybook, "url"},
		{"https://app.wallfacer.ai/handbook/" + testAccountID + "/pages/" + pageBuildID, pageBuildID, kindPage, "url"},
		{"https://app.wallfacer.ai/handbook/" + testAccountID + "/" + playbookBuildID, playbookBuildID, kindPlaybook, "url"},
		{"wallfacer://handbook/pages/" + pageBuildID, pageBuildID, kindPage, "url"},
		{pageBuildID, pageBuildID, kindPage, "id"},
	}

	for _, tc := range cases {
		ref, err := fixture.api().resolveHandbookRef(tc.reference, "")
		if err != nil {
			t.Errorf("%q: %v", tc.reference, err)
			continue
		}
		if ref.ID != tc.wantID || ref.Type != tc.wantKind {
			t.Errorf("%q: got %s %s, want %s %s", tc.reference, ref.Type, ref.ID, tc.wantKind, tc.wantID)
		}
		// resolved_from reports which reference form was accepted, so a URL
		// must not come back as an ID.
		if ref.ResolvedFrom != tc.wantFrom {
			t.Errorf("%q: resolved_from is %q, want %s", tc.reference, ref.ResolvedFrom, tc.wantFrom)
		}
	}
}

func TestResolveRejectsWrongAccountURLBeforeReading(t *testing.T) {
	fixture := newHandbookFixture(t)

	reference := "https://app.wallfacer.ai/accounts/" + otherAccountID + "/handbook/pages/" + pageBuildID
	_, err := fixture.api().resolveHandbookRef(reference, "")
	if err == nil {
		t.Fatal("expected a wrong-account reference to be rejected")
	}
	if !strings.Contains(err.Error(), otherAccountID) || !strings.Contains(err.Error(), testAccountID) {
		t.Errorf("error should name both accounts: %v", err)
	}
	if got := fixture.recorded(); len(got) != 0 {
		t.Errorf("expected the mismatch to be caught before any request, got %d", len(got))
	}
}

func TestResolveRejectsMalformedAndMissingReferences(t *testing.T) {
	fixture := newHandbookFixture(t)

	cases := []struct {
		reference string
		want      string
	}{
		{"", "reference is required"},
		{"https://app.wallfacer.ai/accounts/" + testAccountID + "/tasks/" + pageBuildID, "not a Wallfacer handbook"},
		{"wallfacer://handbook/pages/not-a-uuid", "not a wallfacer:// handbook page reference"},
		{"wallfacer://tasks/" + pageBuildID, "not a wallfacer:// handbook page reference"},
		{"No Such Page", "no active page or playbook named"},
		{unknownID, "no page or playbook found"},
	}

	for _, tc := range cases {
		_, err := fixture.api().resolveHandbookRef(tc.reference, "")
		if err == nil {
			t.Errorf("%q: expected an error", tc.reference)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: error %q does not contain %q", tc.reference, err.Error(), tc.want)
		}
	}
}

func TestResolveRejectsWrongType(t *testing.T) {
	fixture := newHandbookFixture(t)

	_, err := fixture.api().resolveHandbookRef(playbookBuildID, kindPage)
	if err == nil || !strings.Contains(err.Error(), "is a playbook, but a page was requested") {
		t.Fatalf("expected a wrong-type error, got %v", err)
	}

	url := "https://app.wallfacer.ai/accounts/" + testAccountID + "/handbook/pages/" + pageBuildID
	_, err = fixture.api().resolveHandbookRef(url, kindPlaybook)
	if err == nil || !strings.Contains(err.Error(), "names a page, but a playbook was requested") {
		t.Fatalf("expected a wrong-type error for the URL, got %v", err)
	}
}

func TestResolveArchivedAndDeletedEntriesByID(t *testing.T) {
	fixture := newHandbookFixture(t)

	deleted, err := fixture.api().resolveHandbookRef(pageDeletedID, "")
	if err != nil {
		t.Fatalf("resolving a deleted page by ID: %v", err)
	}
	if deleted.State != "deleted" {
		t.Errorf("state is %q, want deleted", deleted.State)
	}

	archived, err := fixture.api().resolveHandbookRef(playbookArchivedID, "")
	if err != nil {
		t.Fatalf("resolving an archived playbook by ID: %v", err)
	}
	if archived.State != "archived" || archived.Type != kindPlaybook {
		t.Errorf("got %s/%s, want playbook/archived", archived.Type, archived.State)
	}

	// Neither is reachable by name: names resolve against active entries.
	if _, err := fixture.api().resolveHandbookRef("Deleted Draft Page", ""); err == nil {
		t.Error("a deleted page should not resolve by name")
	}
}

func TestReadPageReturnsBodyAndFollowUps(t *testing.T) {
	fixture := newHandbookFixture(t)

	output := capture(t, func() error {
		return runHandbookRead(fixture.api(), "Engineering/Build", kindPage)
	})

	data := output["data"].(map[string]interface{})
	if !strings.Contains(data["body"].(string), "How a change reaches") {
		t.Errorf("page body missing from the read: %v", data["body"])
	}

	reference := output["reference"].(map[string]interface{})
	if reference["id"] != pageBuildID || reference["type"] != kindPage {
		t.Errorf("unexpected reference: %v", reference)
	}

	followUp := output["follow_up"].(map[string]interface{})
	for _, key := range []string{"revisions", "parent"} {
		if _, ok := followUp[key]; !ok {
			t.Errorf("follow_up is missing %q: %v", key, followUp)
		}
	}
	assertFollowUpRuns(t, followUp)
}

// A result that names one record has follow_up entries that all run as
// printed: a read holds no revision id, so it names `revisions` rather than a
// `revision` command with a placeholder in it. `next_page` is exempt, since
// which page to ask for is the caller's choice rather than an id the command
// is holding. The many-record listings are a different shape and are covered
// by TestManyRecordListingsFollowUpIsAShape.
func assertFollowUpRuns(t *testing.T, followUp map[string]interface{}) {
	t.Helper()
	for key, value := range followUp {
		if key == "next_page" {
			continue
		}
		commands := []interface{}{value}
		if list, ok := value.([]interface{}); ok {
			commands = list
		}
		for _, command := range commands {
			text, ok := command.(string)
			if !ok {
				t.Errorf("follow_up %q is not a command: %v", key, command)
				continue
			}
			if strings.Contains(text, "<") {
				t.Errorf("follow_up %q is a template, not a runnable command: %s", key, text)
			}
		}
	}
}

// A deleted page is not on the show route, so the read has to go back to the
// include-deleted list the resolver found it in.
func TestReadDeletedPageServesTheListRecord(t *testing.T) {
	fixture := newHandbookFixture(t)

	output := capture(t, func() error {
		return runHandbookRead(fixture.api(), pageDeletedID, "")
	})

	data := output["data"].(map[string]interface{})
	if data["body"] != "Superseded." {
		t.Errorf("deleted page body is %v, want the stored body", data["body"])
	}

	reference := output["reference"].(map[string]interface{})
	if reference["id"] != pageDeletedID || reference["state"] != "deleted" {
		t.Errorf("unexpected reference: %v", reference)
	}

	var sawIncludeDeleted bool
	for _, request := range fixture.recorded() {
		if strings.Contains(request.query, "include_deleted=true") {
			sawIncludeDeleted = true
		}
	}
	if !sawIncludeDeleted {
		t.Error("the read never asked for the include-deleted list")
	}
}

// The deleted-page lookup is exhaustive: a stable ID either belongs to the
// account or does not, so the sweep must not stop at the page-count default
// that bounds search.
func TestDeletedPageLookupWalksPastTheSweepDefault(t *testing.T) {
	const deletedOnPage = 25

	var pagesRead atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if raw := r.URL.Query().Get("page"); raw != "" {
			page, _ = strconv.Atoi(raw)
		}
		pagesRead.Add(1)

		record := fmt.Sprintf(`{"id":"filler-%d","title":"Filler","body":"","deleted_at":null}`, page)
		if page == deletedOnPage {
			record = pageDeletedRecordJSON
		}
		next := ""
		if page < deletedOnPage {
			next = fmt.Sprintf("%s?page=%d", r.URL.Path, page+1)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"data":[%s],"links":{"next":%s},"meta":{"current_page":%d}}`, record, strconv.Quote(next), page)
	}))
	t.Cleanup(server.Close)

	viper.Set("server", server.URL)
	t.Cleanup(func() { viper.Set("server", "") })

	api := &handbookAPI{accountID: testAccountID}
	record, err := api.findDeletedPage(pageDeletedID)
	if err != nil {
		t.Fatalf("finding a deleted page on list page %d: %v", deletedOnPage, err)
	}
	if stringField(record, "id") != pageDeletedID {
		t.Errorf("found %q, want the deleted page", stringField(record, "id"))
	}
	if got := pagesRead.Load(); got != deletedOnPage {
		t.Errorf("read %d list pages, want %d", got, deletedOnPage)
	}
}

func TestReadPlaybookExpandsActiveVersionAndHidesDraftContent(t *testing.T) {
	fixture := newHandbookFixture(t)

	output := capture(t, func() error {
		return runHandbookRead(fixture.api(), playbookBuildID, kindPlaybook)
	})

	data := output["data"].(map[string]interface{})
	active := data["active_version"].(map[string]interface{})
	definition, ok := active["definition"].(map[string]interface{})
	if !ok {
		t.Fatalf("active_version was not expanded to the published definition: %v", active)
	}
	if steps, ok := definition["steps"].([]interface{}); !ok || len(steps) == 0 {
		t.Errorf("published definition has no steps: %v", definition)
	}

	draft := data["draft"].(map[string]interface{})
	if draft["present"] != true {
		t.Errorf("draft presence not reported: %v", draft)
	}
	if _, leaked := draft["definition"]; leaked {
		t.Error("draft content must not be returned by read; use handbook draft")
	}

	followUp := output["follow_up"].(map[string]interface{})
	linked, ok := followUp["linked_pages"].([]interface{})
	if !ok || len(linked) != 1 {
		t.Errorf("follow_up should name a command per linked page: %v", followUp["linked_pages"])
	}
	assertFollowUpRuns(t, followUp)
}

func TestDraftIsReadSeparately(t *testing.T) {
	fixture := newHandbookFixture(t)

	output := capture(t, func() error {
		return runHandbookDraft(fixture.api(), playbookBuildID)
	})

	data := output["data"].(map[string]interface{})
	if data["present"] != true {
		t.Fatalf("expected a draft to be present: %v", data)
	}
	draft := data["draft"].(map[string]interface{})
	if _, ok := draft["definition"].(map[string]interface{}); !ok {
		t.Errorf("draft command should return the draft definition: %v", draft)
	}
}

func TestVersionReadsTheRequestedVersion(t *testing.T) {
	fixture := newHandbookFixture(t)

	// Explicit version, the active version, and a version named by URL all
	// reach the same published definition.
	for _, run := range []func() error{
		func() error { return runHandbookVersion(fixture.api(), playbookBuildID, "2") },
		func() error { return runHandbookVersion(fixture.api(), playbookBuildID, "") },
		func() error {
			url := "https://app.wallfacer.ai/accounts/" + testAccountID + "/handbook/" + playbookBuildID + "/versions/2"
			return runHandbookVersion(fixture.api(), url, "")
		},
	} {
		output := capture(t, run)
		data := output["data"].(map[string]interface{})
		if data["id"] != playbookVersionID {
			t.Errorf("got version %v, want %s", data["id"], playbookVersionID)
		}
	}
}

func TestRevisionsAndRevisionRead(t *testing.T) {
	fixture := newHandbookFixture(t)

	list := capture(t, func() error {
		return runHandbookRevisions(fixture.api(), pageBuildID, 0, 0)
	})
	items := list["data"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("expected 1 revision, got %d", len(items))
	}

	followUp := list["follow_up"].(map[string]interface{})
	want := "wallfacer handbook revision " + pageBuildID + " " + revisionID
	if followUp["revision"] != want {
		t.Errorf("follow_up.revision = %v, want %s", followUp["revision"], want)
	}
	assertFollowUpRuns(t, followUp)

	one := capture(t, func() error {
		return runHandbookRevision(fixture.api(), pageBuildID, revisionID)
	})
	data := one["data"].(map[string]interface{})
	if !strings.Contains(data["body"].(string), "earlier wording") {
		t.Errorf("revision body missing: %v", data["body"])
	}

	if _, err := runHandbookRevisionError(fixture); err == nil {
		t.Error("expected a missing revision to be reported")
	}
}

func runHandbookRevisionError(fixture *handbookFixture) (struct{}, error) {
	return struct{}{}, runHandbookRevision(fixture.api(), pageBuildID, unknownID)
}

func TestListTraversesBeyondTheFirstPage(t *testing.T) {
	fixture := newHandbookFixture(t)

	first := capture(t, func() error {
		return runHandbookList(fixture.api(), kindPage, 0, 0, false, false)
	})
	if len(first["data"].([]interface{})) != 2 {
		t.Fatalf("expected 2 entries on the first page, got %v", first["data"])
	}
	pagination := first["pagination"].(map[string]interface{})["pages"].(map[string]interface{})
	links := pagination["links"].(map[string]interface{})
	if links["next"] == nil {
		t.Error("pagination links must expose the next page")
	}

	second := capture(t, func() error {
		return runHandbookList(fixture.api(), kindPage, 2, 0, false, false)
	})
	entries := second["data"].([]interface{})
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries on the second page, got %d", len(entries))
	}

	var sawPageQuery bool
	for _, request := range fixture.recorded() {
		if strings.HasSuffix(request.path, "/pages") && strings.Contains(request.query, "page=2") {
			sawPageQuery = true
		}
	}
	if !sawPageQuery {
		t.Error("--page 2 should ask the pages endpoint for page 2")
	}
}

func TestVersionsTraverseBeyondTheFirstPage(t *testing.T) {
	fixture := newHandbookFixture(t)

	first := capture(t, func() error {
		return runHandbookVersions(fixture.api(), playbookBuildID, 0, 0)
	})
	links := first["pagination"].(map[string]interface{})["links"].(map[string]interface{})
	if links["next"] == nil {
		t.Error("pagination links must expose the next page")
	}
	firstFollowUp := first["follow_up"].(map[string]interface{})
	if want := "wallfacer handbook version " + playbookBuildID + " 2"; firstFollowUp["version"] != want {
		t.Errorf("follow_up.version = %v, want %s", firstFollowUp["version"], want)
	}
	assertFollowUpRuns(t, firstFollowUp)

	second := capture(t, func() error {
		return runHandbookVersions(fixture.api(), playbookBuildID, 2, 1)
	})
	entries := second["data"].([]interface{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 version on the second page, got %d", len(entries))
	}
	if entries[0].(map[string]interface{})["id"] != playbookVersionFirstID {
		t.Errorf("second page should hold the older version, got %v", entries[0])
	}

	var sawPageQuery bool
	for _, request := range fixture.recorded() {
		if strings.HasSuffix(request.path, "/versions") && strings.Contains(request.query, "page=2") && strings.Contains(request.query, "per_page=1") {
			sawPageQuery = true
		}
	}
	if !sawPageQuery {
		t.Error("--page and --per-page should reach the versions endpoint")
	}
}

func TestListCarriesPathsAndArchivedState(t *testing.T) {
	fixture := newHandbookFixture(t)

	output := capture(t, func() error {
		return runHandbookList(fixture.api(), kindPlaybook, 0, 0, false, true)
	})

	states := map[string]string{}
	paths := map[string]string{}
	for _, item := range output["data"].([]interface{}) {
		entry := item.(map[string]interface{})
		states[entry["id"].(string)] = entry["state"].(string)
		if path, ok := entry["path"].(string); ok {
			paths[entry["id"].(string)] = path
		}
	}

	if states[playbookArchivedID] != "archived" {
		t.Errorf("archived playbook state is %q", states[playbookArchivedID])
	}
	if paths[playbookBuildID] != "Engineering/Build" {
		t.Errorf("active playbook path is %q, want Engineering/Build", paths[playbookBuildID])
	}
}

// `tree`, `list` and `search` name many records at once, so their follow_up
// entries give the command's shape and the caller fills the reference in from
// the record they picked. That is the documented exception to entries running
// as printed, so pin it rather than leave it to drift either way.
func TestManyRecordListingsFollowUpIsAShape(t *testing.T) {
	fixture := newHandbookFixture(t)

	listings := map[string]func() error{
		"tree": func() error { return runHandbookTree(fixture.api()) },
		"list": func() error { return runHandbookList(fixture.api(), "", 0, 0, false, false) },
		"search": func() error {
			return runHandbookSearch(fixture.api(), "build", "", 20, 20)
		},
	}

	for name, run := range listings {
		t.Run(name, func(t *testing.T) {
			followUp := capture(t, run)["follow_up"].(map[string]interface{})
			if _, ok := followUp["read"]; !ok {
				t.Fatalf("%s follow_up is missing a read entry: %v", name, followUp)
			}
			read := followUp["read"].(string)
			if !strings.Contains(read, "<") {
				t.Errorf("%s follow_up.read names a concrete record (%s); the docs say these are shapes", name, read)
			}
		})
	}
}

func TestSearchFindsMatchesBeyondTheFirstPage(t *testing.T) {
	fixture := newHandbookFixture(t)

	output := capture(t, func() error {
		return runHandbookSearch(fixture.api(), "grooming", "", 20, 20)
	})

	matches := output["data"].([]interface{})
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d: %v", len(matches), matches)
	}
	match := matches[0].(map[string]interface{})
	if match["id"] != pageProductID {
		t.Errorf("matched %v, want the page that only appears on page 2", match["id"])
	}
	matchedIn := match["matched_in"].([]interface{})
	if len(matchedIn) == 0 || matchedIn[0] != "body" {
		t.Errorf("matched_in is %v, want body", matchedIn)
	}

	sweep := output["pagination"].(map[string]interface{})["pages"].(map[string]interface{})
	if sweep["complete"] != true {
		t.Errorf("sweep should report itself complete: %v", sweep)
	}
	if sweep["pages_read"].(float64) != 2 {
		t.Errorf("sweep read %v pages, want 2", sweep["pages_read"])
	}
}

func TestSearchMatchesBothTypes(t *testing.T) {
	fixture := newHandbookFixture(t)

	output := capture(t, func() error {
		return runHandbookSearch(fixture.api(), "build", "", 20, 20)
	})

	kinds := map[string]bool{}
	for _, item := range output["data"].([]interface{}) {
		kinds[item.(map[string]interface{})["type"].(string)] = true
	}
	if !kinds[kindPage] || !kinds[kindPlaybook] {
		t.Errorf("search should return both pages and playbooks, got %v", kinds)
	}
}

// "build" matches a page and a playbook, so a limit of 1 has to stop the
// second sweep rather than let its first match through.
func TestSearchLimitIsAHardMaximum(t *testing.T) {
	fixture := newHandbookFixture(t)

	output := capture(t, func() error {
		return runHandbookSearch(fixture.api(), "build", "", 1, 20)
	})

	data := output["data"].([]interface{})
	if len(data) != 1 {
		t.Fatalf("search returned %d rows, want 1", len(data))
	}
	if kind := data[0].(map[string]interface{})["type"].(string); kind != kindPage {
		t.Errorf("first match is a %s, want %s", kind, kindPage)
	}
}

func TestDiscoveryCommandsOnlyRead(t *testing.T) {
	fixture := newHandbookFixture(t)

	commands := []func() error{
		func() error { return runHandbookTree(fixture.api()) },
		func() error { return runHandbookList(fixture.api(), "", 0, 0, true, true) },
		func() error { return runHandbookSearch(fixture.api(), "build", "", 20, 20) },
		func() error { return runHandbookRead(fixture.api(), pageBuildID, "") },
		func() error { return runHandbookRead(fixture.api(), playbookBuildID, "") },
		func() error { return runHandbookResolve(fixture.api(), "Engineering/Build", kindPage) },
		func() error { return runHandbookRevisions(fixture.api(), pageBuildID, 0, 0) },
		func() error { return runHandbookRevision(fixture.api(), pageBuildID, revisionID) },
		func() error { return runHandbookVersions(fixture.api(), playbookBuildID, 0, 0) },
		func() error { return runHandbookVersion(fixture.api(), playbookBuildID, "2") },
		func() error { return runHandbookDraft(fixture.api(), playbookBuildID) },
	}

	for _, command := range commands {
		capture(t, command)
	}

	recorded := fixture.recorded()
	if len(recorded) == 0 {
		t.Fatal("expected the commands to issue requests")
	}
	for _, request := range recorded {
		if request.method != http.MethodGet {
			t.Errorf("%s %s: discovery must never issue anything but GET", request.method, request.path)
		}
		if strings.Contains(request.path, "/tasks") {
			t.Errorf("%s must not touch tasks", request.path)
		}
	}
}

func TestOutputStaysProjectableWithQuery(t *testing.T) {
	fixture := newHandbookFixture(t)

	viper.Set("query", "reference.id")
	defer viper.Set("query", "")

	var buf bytes.Buffer
	previous := cli.Stdout
	cli.Stdout = &buf
	defer func() { cli.Stdout = previous }()

	if err := runHandbookRead(fixture.api(), pageBuildID, ""); err != nil {
		t.Fatalf("read failed: %v", err)
	}

	if got := strings.TrimSpace(buf.String()); got != `"`+pageBuildID+`"` {
		t.Errorf("query projection returned %s, want the page ID", got)
	}
}

func wrapData(record string) string {
	return `{"data":` + record + `}`
}

const handbookTreeJSON = `{"data":{"tree":[
  {"type":"page","id":"` + pageEngineeringID + `","title":"Engineering","description":"How we build and ship.","position":0,"has_body":true,"updated_at":"2026-09-01T00:00:00.000000Z","children":[
    {"type":"page","id":"` + pageBuildID + `","title":"Build","description":"From issue to merge.","position":0,"has_body":true,"updated_at":"2026-09-01T00:00:00.000000Z","children":[]},
    {"type":"playbook","id":"` + playbookBuildID + `","title":"Build","description":"Implement an assigned issue.","position":1,"disabled_at":null,"has_draft":true,"active_version":{"id":"` + playbookVersionID + `","version":2,"steps":[{"id":"implement","title":"Implement the issue","kind":"ai"}]},"task_count":12},
    {"type":"page","id":"` + pageReviewEngID + `","title":"Review","description":"Checking a prepared pull request.","position":2,"has_body":true,"updated_at":"2026-09-01T00:00:00.000000Z","children":[]}
  ]},
  {"type":"page","id":"` + pageProductID + `","title":"Product","description":"What we build and why.","position":1,"has_body":true,"updated_at":"2026-09-01T00:00:00.000000Z","children":[
    {"type":"page","id":"` + pageReviewProdID + `","title":"Review","description":"Reviewing an idea.","position":0,"has_body":true,"updated_at":"2026-09-01T00:00:00.000000Z","children":[]}
  ]}
]}}`

const pageBuildRecordJSON = `{"id":"` + pageBuildID + `","account_id":"` + testAccountID + `","parent_page_id":"` + pageEngineeringID + `","title":"Build","body":"How a change reaches the product: issue, pull request, review, merge.","description":"From issue to merge.","position":0,"created_by":1,"created_at":"2026-08-01T00:00:00.000000Z","updated_at":"2026-09-01T00:00:00.000000Z","deleted_at":null}`

const pageCreatedRecordJSON = `{"id":"` + pageCreatedID + `","account_id":"` + testAccountID + `","parent_page_id":"` + pageEngineeringID + `","title":"Writing Great PRs","body":"Lead with the problem.","description":null,"position":3,"created_by":1,"created_at":"2026-09-18T00:00:00.000000Z","updated_at":"2026-09-18T00:00:00.000000Z","deleted_at":null}`

const pageRestoredRecordJSON = `{"id":"` + pageDeletedID + `","account_id":"` + testAccountID + `","parent_page_id":null,"title":"Deleted Draft Page","body":"Superseded.","description":null,"position":0,"created_by":1,"created_at":"2026-08-01T00:00:00.000000Z","updated_at":"2026-09-18T00:00:00.000000Z","deleted_at":null}`

const pageDeletedRecordJSON = `{"id":"` + pageDeletedID + `","account_id":"` + testAccountID + `","parent_page_id":null,"title":"Deleted Draft Page","body":"Superseded.","description":null,"position":0,"created_by":1,"created_at":"2026-08-01T00:00:00.000000Z","updated_at":"2026-08-02T00:00:00.000000Z","deleted_at":"2026-08-03T00:00:00.000000Z"}`

const pagesPageOneJSON = `{"data":[
  {"id":"` + pageEngineeringID + `","account_id":"` + testAccountID + `","parent_page_id":null,"title":"Engineering","body":"How we build and ship.","description":"How we build and ship.","position":0,"deleted_at":null},
  ` + pageBuildRecordJSON + `
],"links":{"first":"http://example.test/pages?page=1","last":"http://example.test/pages?page=2","prev":null,"next":"http://example.test/pages?page=2"},"meta":{"current_page":1,"last_page":2,"per_page":2,"total":5}}`

const pagesPageTwoJSON = `{"data":[
  {"id":"` + pageReviewEngID + `","account_id":"` + testAccountID + `","parent_page_id":"` + pageEngineeringID + `","title":"Review","body":"A fresh reader catches what the author cannot.","description":"Checking a prepared pull request.","position":2,"deleted_at":null},
  {"id":"` + pageProductID + `","account_id":"` + testAccountID + `","parent_page_id":null,"title":"Product","body":"Idea intake, research, and backlog grooming.","description":"What we build and why.","position":1,"deleted_at":null},
  {"id":"` + pageReviewProdID + `","account_id":"` + testAccountID + `","parent_page_id":"` + pageProductID + `","title":"Review","body":"Reviewing an idea before it becomes work.","description":"Reviewing an idea.","position":0,"deleted_at":null}
],"links":{"first":"http://example.test/pages?page=1","last":"http://example.test/pages?page=2","prev":"http://example.test/pages?page=1","next":null},"meta":{"current_page":2,"last_page":2,"per_page":2,"total":5}}`

// The API binds `GET /pages/{page}` to active rows only, so a deleted page is
// reachable through the list endpoint with include_deleted=true and nowhere
// else. The fixture mirrors that: the deleted record appears here and the
// single-page route 404s on its ID.
const pagesPageTwoWithDeletedJSON = `{"data":[
  {"id":"` + pageReviewEngID + `","account_id":"` + testAccountID + `","parent_page_id":"` + pageEngineeringID + `","title":"Review","body":"A fresh reader catches what the author cannot.","description":"Checking a prepared pull request.","position":2,"deleted_at":null},
  {"id":"` + pageProductID + `","account_id":"` + testAccountID + `","parent_page_id":null,"title":"Product","body":"Idea intake, research, and backlog grooming.","description":"What we build and why.","position":1,"deleted_at":null},
  {"id":"` + pageReviewProdID + `","account_id":"` + testAccountID + `","parent_page_id":"` + pageProductID + `","title":"Review","body":"Reviewing an idea before it becomes work.","description":"Reviewing an idea.","position":0,"deleted_at":null},
  ` + pageDeletedRecordJSON + `
],"links":{"first":"http://example.test/pages?page=1","last":"http://example.test/pages?page=2","prev":"http://example.test/pages?page=1","next":null},"meta":{"current_page":2,"last_page":2,"per_page":2,"total":6}}`

const playbookRecordJSON = `{"id":"` + playbookBuildID + `","account_id":"` + testAccountID + `","name":"Build","description":"Implement an assigned issue.","active_version":{"id":"` + playbookVersionID + `","version":2,"created_at":"2026-09-01T00:00:00.000000Z"},"version_count":2,"draft":{"definition":{"steps":[{"id":"implement","kind":"ai","title":"Implement the issue, revised"}]},"updated_at":"2026-09-10T00:00:00.000000Z","updated_by":2},"parent_page_id":"` + pageEngineeringID + `","position":1,"linked_page_ids":["` + pageBuildID + `"],"disabled_at":null,"archived_at":null,"created_at":"2026-08-01T00:00:00.000000Z","created_by":1}`

const playbookArchivedRecordJSON = `{"id":"` + playbookArchivedID + `","account_id":"` + testAccountID + `","name":"Retired Playbook","description":null,"active_version":{"id":"` + playbookVersionID + `","version":1},"version_count":1,"draft":null,"parent_page_id":null,"position":9,"linked_page_ids":[],"disabled_at":"2026-08-20T00:00:00.000000Z","archived_at":"2026-08-21T00:00:00.000000Z","created_at":"2026-08-01T00:00:00.000000Z","created_by":1}`

const pipelinesJSON = `{"data":[` + playbookRecordJSON + `],"links":{"first":"http://example.test/pipelines?page=1","last":"http://example.test/pipelines?page=1","prev":null,"next":null},"meta":{"current_page":1,"last_page":1,"per_page":25,"total":1}}`

const pipelinesWithArchivedJSON = `{"data":[` + playbookRecordJSON + `,` + playbookArchivedRecordJSON + `],"links":{"first":"http://example.test/pipelines?page=1","last":"http://example.test/pipelines?page=1","prev":null,"next":null},"meta":{"current_page":1,"last_page":1,"per_page":25,"total":2}}`

const emptyListJSON = `{"data":[],"links":{"first":null,"last":null,"prev":null,"next":null},"meta":{"current_page":2,"last_page":1,"per_page":25,"total":0}}`

const playbookVersionRecordJSON = `{"id":"` + playbookVersionID + `","pipeline_id":"` + playbookBuildID + `","version":2,"definition":{"format_version":1,"description":"Implement an assigned issue.","steps":[{"id":"implement","kind":"ai","title":"Implement the issue","content":"Read the issue and implement it."}],"triggers":[]},"notes":"Second cut.","created_at":"2026-09-01T00:00:00.000000Z","created_by":1}`

const playbookVersionFirstRecordJSON = `{"id":"` + playbookVersionFirstID + `","pipeline_id":"` + playbookBuildID + `","version":1,"definition":{"format_version":1,"description":"Implement an assigned issue.","steps":[{"id":"implement","kind":"ai","title":"Implement the issue"}],"triggers":[]},"notes":"First cut.","created_at":"2026-08-01T00:00:00.000000Z","created_by":1}`

const playbookVersionsJSON = `{"data":[` + playbookVersionRecordJSON + `],"links":{"first":"http://example.test/versions?page=1","last":"http://example.test/versions?page=2","prev":null,"next":"http://example.test/versions?page=2"},"meta":{"current_page":1,"last_page":2,"per_page":1,"total":2}}`

const playbookVersionsPageTwoJSON = `{"data":[` + playbookVersionFirstRecordJSON + `],"links":{"first":"http://example.test/versions?page=1","last":"http://example.test/versions?page=2","prev":"http://example.test/versions?page=1","next":null},"meta":{"current_page":2,"last_page":2,"per_page":1,"total":2}}`

const revisionRecordJSON = `{"id":"` + revisionID + `","page_id":"` + pageBuildID + `","title":"Build","body":"The earlier wording of the build page.","description":"From issue to merge.","created_at":"2026-08-15T00:00:00.000000Z","created_by":1}`

const revisionsJSON = `{"data":[` + revisionRecordJSON + `],"links":{"first":"http://example.test/revisions?page=1","last":"http://example.test/revisions?page=1","prev":null,"next":null},"meta":{"current_page":1,"last_page":1,"per_page":25,"total":1}}`
