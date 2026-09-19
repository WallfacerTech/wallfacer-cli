package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/viper"
)

const (
	playbookPublishedOnlyID = "bbbbbbb2-1111-4111-8111-111111111111"
	playbookUnpublishedID   = "bbbbbbb3-1111-4111-8111-111111111111"
	playbookDisabledID      = "bbbbbbb4-1111-4111-8111-111111111111"
	playbookInvalidDraftID  = "bbbbbbb5-1111-4111-8111-111111111111"
	playbookRejectedID      = "bbbbbbb6-1111-4111-8111-111111111111"
	playbookCreatedID       = "bbbbbbb7-1111-4111-8111-111111111111"

	newVersionID = "ccccccc2-1111-4111-8111-111111111111"
)

// authoringFixture serves the playbook states the draft-and-publish commands
// have to tell apart: a draft alongside an active version, published with no
// draft, a draft with nothing published yet, a disabled playbook, a draft the
// server will reject, and a draft stored verbatim that holds no definition.
type authoringFixture struct {
	server *httptest.Server

	mu       sync.Mutex
	requests []recordedRequest
	bodies   map[string][]string

	// published records the publish calls that were accepted, so a test can
	// assert that a failed or refused publish created nothing.
	published int
	discarded int
}

func newAuthoringFixture(t *testing.T) *authoringFixture {
	t.Helper()

	fixture := &authoringFixture{bodies: map[string][]string{}}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		fixture.mu.Lock()
		fixture.requests = append(fixture.requests, recordedRequest{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
		})
		key := r.Method + " " + r.URL.Path
		fixture.bodies[key] = append(fixture.bodies[key], string(body))
		fixture.mu.Unlock()

		payload, status := fixture.route(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, payload)
	}))
	t.Cleanup(fixture.server.Close)

	viper.Set("server", fixture.server.URL)
	t.Cleanup(func() { viper.Set("server", "") })

	return fixture
}

func (f *authoringFixture) route(r *http.Request) (string, int) {
	base := "/v1/accounts/" + testAccountID

	switch {
	case r.URL.Path == base+"/handbook":
		return handbookTreeJSON, http.StatusOK

	case r.URL.Path == base+"/pages":
		return pagesPageOneJSON, http.StatusOK
	case r.URL.Path == base+"/pages/"+pageBuildID:
		return wrapData(pageBuildRecordJSON), http.StatusOK

	case r.URL.Path == base+"/pipelines" && r.Method == http.MethodPost:
		return wrapData(playbookCreatedRecordJSON), http.StatusCreated
	case r.URL.Path == base+"/pipelines/"+playbookCreatedID+"/draft" && r.Method == http.MethodPut:
		return "", http.StatusNoContent
	case r.URL.Path == base+"/pipelines":
		return pipelinesJSON, http.StatusOK

	// Draft alongside an active version.
	case r.URL.Path == base+"/pipelines/"+playbookBuildID && r.Method == http.MethodGet:
		return wrapData(playbookRecordJSON), http.StatusOK
	case r.URL.Path == base+"/pipelines/"+playbookBuildID && r.Method == http.MethodPatch:
		return wrapData(playbookRecordJSON), http.StatusOK
	case r.URL.Path == base+"/pipelines/"+playbookBuildID && r.Method == http.MethodDelete:
		return "", http.StatusNoContent
	// The draft endpoint answers with the whole playbook record.
	case r.URL.Path == base+"/pipelines/"+playbookBuildID+"/draft" && r.Method == http.MethodPut:
		return wrapData(playbookRecordJSON), http.StatusOK
	case r.URL.Path == base+"/pipelines/"+playbookBuildID+"/draft" && r.Method == http.MethodDelete:
		f.mu.Lock()
		f.discarded++
		f.mu.Unlock()
		return "", http.StatusNoContent
	case r.URL.Path == base+"/pipelines/"+playbookBuildID+"/versions" && r.Method == http.MethodPost:
		f.mu.Lock()
		f.published++
		f.mu.Unlock()
		return wrapData(newVersionRecordJSON), http.StatusCreated
	case r.URL.Path == base+"/pipelines/"+playbookBuildID+"/versions/2":
		return wrapData(playbookVersionRecordJSON), http.StatusOK
	case r.URL.Path == base+"/pipelines/"+playbookBuildID+"/versions/2/diff/3":
		return versionDiffJSON, http.StatusOK

	// Published, with no draft saved.
	case r.URL.Path == base+"/pipelines/"+playbookPublishedOnlyID:
		return wrapData(playbookPublishedOnlyRecordJSON), http.StatusOK

	// A draft, with nothing published yet.
	case r.URL.Path == base+"/pipelines/"+playbookUnpublishedID && r.Method == http.MethodGet:
		return wrapData(playbookUnpublishedRecordJSON), http.StatusOK
	case r.URL.Path == base+"/pipelines/"+playbookUnpublishedID+"/versions" && r.Method == http.MethodPost:
		f.mu.Lock()
		f.published++
		f.mu.Unlock()
		return wrapData(firstVersionRecordJSON), http.StatusCreated

	// Disabled, with a draft to publish.
	case r.URL.Path == base+"/pipelines/"+playbookDisabledID && r.Method == http.MethodGet:
		return wrapData(playbookDisabledRecordJSON), http.StatusOK
	case r.URL.Path == base+"/pipelines/"+playbookDisabledID+"/versions" && r.Method == http.MethodPost:
		f.mu.Lock()
		f.published++
		f.mu.Unlock()
		return wrapData(newVersionRecordJSON), http.StatusCreated

	// Archived, so it is outside the tree and only the record says so.
	case r.URL.Path == base+"/pipelines/"+playbookArchivedID && r.Method == http.MethodGet:
		return wrapData(playbookArchivedRecordJSON), http.StatusOK
	case r.URL.Path == base+"/pipelines/"+playbookArchivedID && r.Method == http.MethodPatch:
		return wrapData(playbookRestoredRecordJSON), http.StatusOK

	// A draft stored verbatim that holds no definition object.
	case r.URL.Path == base+"/pipelines/"+playbookInvalidDraftID:
		return wrapData(playbookInvalidDraftRecordJSON), http.StatusOK

	// A draft the server rejects at publish time.
	case r.URL.Path == base+"/pipelines/"+playbookRejectedID && r.Method == http.MethodGet:
		return wrapData(playbookRejectedRecordJSON), http.StatusOK
	case r.URL.Path == base+"/pipelines/"+playbookRejectedID+"/versions" && r.Method == http.MethodPost:
		return definitionRejectedJSON, http.StatusUnprocessableEntity
	}

	return `{"errors":[{"message":"Not found","code":"not_found"}]}`, http.StatusNotFound
}

func (f *authoringFixture) recorded() []recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedRequest(nil), f.requests...)
}

func (f *authoringFixture) bodyOf(t *testing.T, method, path string) map[string]interface{} {
	t.Helper()

	f.mu.Lock()
	bodies := f.bodies[method+" /v1/accounts/"+testAccountID+path]
	f.mu.Unlock()

	if len(bodies) == 0 {
		t.Fatalf("no %s %s request was made", method, path)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(bodies[len(bodies)-1]), &decoded); err != nil {
		t.Fatalf("%s %s body is not JSON: %v\n%s", method, path, err, bodies[len(bodies)-1])
	}
	return decoded
}

func (f *authoringFixture) api() *handbookAPI {
	return &handbookAPI{accountID: testAccountID}
}

// methodsFor reports the methods used against paths containing the fragment.
func (f *authoringFixture) methodsFor(fragment string) []string {
	methods := []string{}
	for _, request := range f.recorded() {
		if strings.Contains(request.path, fragment) {
			methods = append(methods, request.method+" "+request.path)
		}
	}
	return methods
}

func (f *authoringFixture) counts() (published, discarded int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.published, f.discarded
}

// captureError runs a command body expected to fail and returns its error. A
// command that fails prints nothing, so there is no output to capture.
func captureError(t *testing.T, run func() error) error {
	t.Helper()

	err := run()
	if err == nil {
		t.Fatal("expected the command to fail")
	}
	return err
}

func TestSaveDraftStoresTheDraftWithoutPublishing(t *testing.T) {
	fixture := newAuthoringFixture(t)

	definition := map[string]interface{}{
		"format_version": float64(1),
		"steps":          []interface{}{map[string]interface{}{"id": "implement", "kind": "ai", "title": "Implement the issue, revised"}},
	}

	output := capture(t, func() error {
		return runPlaybookSaveDraft(fixture.api(), playbookBuildID, definition)
	})

	data := output["data"].(map[string]interface{})
	if data["saved"] != true || data["published"] != false {
		t.Errorf("a draft save must report itself as saved and unpublished: %v", data)
	}
	if active := data["active_version"].(map[string]interface{}); active["version"].(float64) != 2 {
		t.Errorf("the active version must be reported unchanged: %v", active)
	}

	// The save answers with the playbook record; what is reported is the
	// draft inside it, not the playbook wrapped as one.
	draft := data["draft"].(map[string]interface{})
	if _, ok := draft["definition"].(map[string]interface{}); !ok {
		t.Errorf("the saved draft must be reported with its definition: %v", draft)
	}
	if _, nested := draft["draft"]; nested {
		t.Errorf("the playbook record must not be reported as the draft: %v", draft)
	}

	body := fixture.bodyOf(t, http.MethodPut, "/pipelines/"+playbookBuildID+"/draft")
	if _, ok := body["definition"].(map[string]interface{}); !ok {
		t.Errorf("the draft request must carry the definition: %v", body)
	}

	for _, request := range fixture.recorded() {
		if request.method == http.MethodPost || request.method == http.MethodDelete {
			t.Errorf("saving a draft must not %s %s", request.method, request.path)
		}
	}
	if published, discarded := fixture.counts(); published != 0 || discarded != 0 {
		t.Errorf("saving a draft published %d versions and discarded %d drafts", published, discarded)
	}
}

func TestDiffDraftOnlyReadsAndReportsChanges(t *testing.T) {
	fixture := newAuthoringFixture(t)

	output := capture(t, func() error {
		return runPlaybookDiffDraft(fixture.api(), playbookBuildID)
	})

	data := output["data"].(map[string]interface{})
	if data["identical"] != false {
		t.Errorf("the fixture's draft differs from the active version: %v", data)
	}
	if data["published"] != false {
		t.Errorf("a diff must report that nothing was published: %v", data)
	}

	changes := data["changes"].([]interface{})
	paths := map[string]string{}
	for _, item := range changes {
		change := item.(map[string]interface{})
		paths[change["path"].(string)] = change["change"].(string)
	}
	if paths["steps[0].title"] != "changed" {
		t.Errorf("the changed step title is missing from the diff: %v", paths)
	}
	if paths["steps[0].content"] != "removed" {
		t.Errorf("a field the draft drops should read as removed: %v", paths)
	}

	for _, request := range fixture.recorded() {
		if request.method != http.MethodGet {
			t.Errorf("comparing a draft must not %s %s", request.method, request.path)
		}
	}
}

func TestDiffDraftWithNoPublishedVersion(t *testing.T) {
	fixture := newAuthoringFixture(t)

	output := capture(t, func() error {
		return runPlaybookDiffDraft(fixture.api(), playbookUnpublishedID)
	})

	data := output["data"].(map[string]interface{})
	if data["active_version"] != nil {
		t.Errorf("there is no active version to report: %v", data["active_version"])
	}
	if note, _ := data["note"].(string); !strings.Contains(note, "no published version") {
		t.Errorf("the absence of a published version should be stated: %v", data["note"])
	}
	for _, item := range data["changes"].([]interface{}) {
		if change := item.(map[string]interface{}); change["change"] != "added" {
			t.Errorf("every field of the draft is new here: %v", change)
		}
	}
}

func TestPublishSendsTheSavedDraftToTheVersionEndpoint(t *testing.T) {
	fixture := newAuthoringFixture(t)

	output := capture(t, func() error {
		return runPlaybookPublish(fixture.api(), playbookBuildID, "Revised the implement step.", true)
	})

	body := fixture.bodyOf(t, http.MethodPost, "/pipelines/"+playbookBuildID+"/versions")
	definition := body["definition"].(map[string]interface{})
	steps := definition["steps"].([]interface{})
	if title := steps[0].(map[string]interface{})["title"]; title != "Implement the issue, revised" {
		t.Errorf("publish sent %v, want the saved draft's definition", title)
	}
	if body["notes"] != "Revised the implement step." {
		t.Errorf("notes were not passed through: %v", body["notes"])
	}
	if _, present := body["activate"]; present {
		t.Errorf("activation is the server's default and should not be restated: %v", body)
	}

	data := output["data"].(map[string]interface{})
	version := data["published_version"].(map[string]interface{})
	if version["id"] != newVersionID {
		t.Errorf("the published version's identifiers must come back: %v", version)
	}
	if data["activated"] != true {
		t.Errorf("activated should be true: %v", data)
	}

	if published, _ := fixture.counts(); published != 1 {
		t.Errorf("publish made %d version calls, want 1", published)
	}
	for _, request := range fixture.recorded() {
		if request.method == http.MethodDelete {
			t.Errorf("publish must not delete anything: %s", request.path)
		}
	}
}

func TestPublishWithoutADraftClaimsNoVersion(t *testing.T) {
	fixture := newAuthoringFixture(t)

	err := captureError(t, func() error {
		return runPlaybookPublish(fixture.api(), playbookPublishedOnlyID, "", true)
	})
	if !strings.Contains(err.Error(), "no saved draft") {
		t.Errorf("error should say there is no draft: %v", err)
	}

	if published, _ := fixture.counts(); published != 0 {
		t.Errorf("a publish with no draft created %d versions", published)
	}
	for _, request := range fixture.recorded() {
		if request.method != http.MethodGet {
			t.Errorf("a refused publish must not %s %s", request.method, request.path)
		}
	}
}

func TestPublishRejectsADraftHoldingNoDefinition(t *testing.T) {
	fixture := newAuthoringFixture(t)

	err := captureError(t, func() error {
		return runPlaybookPublish(fixture.api(), playbookInvalidDraftID, "", true)
	})
	if !strings.Contains(err.Error(), "no definition object") {
		t.Errorf("error should name the invalid draft: %v", err)
	}
	if published, _ := fixture.counts(); published != 0 {
		t.Errorf("an invalid draft was published %d times", published)
	}
}

func TestPublishValidationFailureReportsNoNewVersion(t *testing.T) {
	fixture := newAuthoringFixture(t)

	err := captureError(t, func() error {
		return runPlaybookPublish(fixture.api(), playbookRejectedID, "", true)
	})

	if !strings.Contains(err.Error(), "no version was created") {
		t.Errorf("a validation failure must not read as a publication: %v", err)
	}
	if !strings.Contains(err.Error(), "definition_validation_failed") {
		t.Errorf("the server's own validation errors should be passed through: %v", err)
	}
	if published, _ := fixture.counts(); published != 0 {
		t.Errorf("a rejected publish counted %d versions", published)
	}
}

func TestPublishWithoutActivationSeparatesTheVersions(t *testing.T) {
	fixture := newAuthoringFixture(t)

	output := capture(t, func() error {
		return runPlaybookPublish(fixture.api(), playbookBuildID, "", false)
	})

	body := fixture.bodyOf(t, http.MethodPost, "/pipelines/"+playbookBuildID+"/versions")
	if body["activate"] != false {
		t.Errorf("--activate=false must reach the request: %v", body)
	}

	data := output["data"].(map[string]interface{})
	if data["activated"] != false {
		t.Errorf("activated should be false: %v", data)
	}
	published := data["published_version"].(map[string]interface{})
	active := data["active_version"].(map[string]interface{})
	if published["version"] == active["version"] {
		t.Errorf("an inactive publication must report the new version apart from the active one: %v vs %v", published["version"], active["version"])
	}
	if note, _ := data["note"].(string); !strings.Contains(note, "without activating") {
		t.Errorf("the distinction should be stated: %v", data["note"])
	}
}

func TestPublishLeavesADisabledPlaybookDisabled(t *testing.T) {
	fixture := newAuthoringFixture(t)

	output := capture(t, func() error {
		return runPlaybookPublish(fixture.api(), playbookDisabledID, "", true)
	})

	data := output["data"].(map[string]interface{})
	if data["disabled"] != true {
		t.Errorf("the playbook is still disabled after publishing: %v", data)
	}
	if note, _ := data["disabled_note"].(string); !strings.Contains(note, "--enable") {
		t.Errorf("the way to enable it should be named: %v", data["disabled_note"])
	}

	reference := output["reference"].(map[string]interface{})
	if reference["state"] != "disabled" {
		t.Errorf("reference state is %v, want disabled", reference["state"])
	}

	for _, request := range fixture.recorded() {
		if request.method == http.MethodPatch {
			t.Errorf("publishing must not patch the playbook's enabled state: %s", request.path)
		}
	}
}

func TestPublishFirstVersionOfAnUnpublishedPlaybook(t *testing.T) {
	fixture := newAuthoringFixture(t)

	output := capture(t, func() error {
		return runPlaybookPublish(fixture.api(), playbookUnpublishedID, "", true)
	})

	data := output["data"].(map[string]interface{})
	version := data["published_version"].(map[string]interface{})
	if version["version"].(float64) != 1 {
		t.Errorf("the first publication is version 1: %v", version)
	}
}

func TestDiscardDraftLeavesThePublishedDefinition(t *testing.T) {
	fixture := newAuthoringFixture(t)

	output := capture(t, func() error {
		return runPlaybookDiscardDraft(fixture.api(), playbookBuildID)
	})

	data := output["data"].(map[string]interface{})
	if data["discarded"] != true {
		t.Errorf("discard should report itself: %v", data)
	}
	if active := data["active_version"].(map[string]interface{}); active["version"].(float64) != 2 {
		t.Errorf("the published definition must be untouched: %v", active)
	}

	if _, discarded := fixture.counts(); discarded != 1 {
		t.Errorf("discard made %d calls to the draft endpoint, want 1", discarded)
	}
	if published, _ := fixture.counts(); published != 0 {
		t.Errorf("discard published %d versions", published)
	}

	var sawDraftDelete bool
	for _, request := range fixture.recorded() {
		if request.method == http.MethodDelete {
			if !strings.HasSuffix(request.path, "/draft") {
				t.Errorf("discard deleted %s, which is not the draft", request.path)
			}
			sawDraftDelete = true
		}
	}
	if !sawDraftDelete {
		t.Error("discard must use the draft's own delete operation")
	}
}

func TestVersionDiffUsesTheDiffEndpoint(t *testing.T) {
	fixture := newAuthoringFixture(t)

	output := capture(t, func() error {
		return runPlaybookDiff(fixture.api(), playbookBuildID, "2", "3")
	})

	// The endpoint answers under `data` and the CLI wraps once more, so the
	// diff has to arrive at data.changes rather than data.data.changes.
	data, ok := output["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("the diff response should be passed through: %v", output)
	}
	if _, nested := data["data"]; nested {
		t.Errorf("the diff response must not be wrapped twice: %v", data)
	}
	if changes, ok := data["changes"].([]interface{}); !ok || len(changes) != 1 {
		t.Errorf("the diff's changes must survive unwrapping: %v", data)
	}

	var sawDiff bool
	for _, request := range fixture.recorded() {
		if request.method != http.MethodGet {
			t.Errorf("a diff must not %s %s", request.method, request.path)
		}
		if strings.Contains(request.path, "/diff/") {
			sawDiff = true
		}
	}
	if !sawDiff {
		t.Error("the diff endpoint was never called")
	}
}

func TestCreatePlaybookReportsItsInitialVersion(t *testing.T) {
	fixture := newAuthoringFixture(t)

	definition := map[string]interface{}{
		"format_version": float64(1),
		"steps":          []interface{}{map[string]interface{}{"id": "implement", "kind": "ai", "title": "Implement the issue"}},
	}

	output := capture(t, func() error {
		return runPlaybookCreate(fixture.api(), "New Playbook", "A description.", "Engineering/Build", []string{pageBuildID}, definition, false)
	})

	body := fixture.bodyOf(t, http.MethodPost, "/pipelines")
	if body["name"] != "New Playbook" {
		t.Errorf("name not sent: %v", body)
	}
	if body["parent_page_id"] != pageBuildID {
		t.Errorf("the parent reference should be resolved to an ID: %v", body["parent_page_id"])
	}
	if linked := body["linked_page_ids"].([]interface{}); len(linked) != 1 || linked[0] != pageBuildID {
		t.Errorf("linked pages not resolved: %v", body["linked_page_ids"])
	}
	if _, ok := body["definition"].(map[string]interface{}); !ok {
		t.Errorf("the definition must be sent on create: %v", body)
	}

	data := output["data"].(map[string]interface{})
	active := data["active_version"].(map[string]interface{})
	if active["version"].(float64) != 1 {
		t.Errorf("create publishes version 1: %v", active)
	}

	// Creating publishes on its own; it must not also call the version
	// endpoint or save a draft nobody asked for.
	for _, request := range fixture.recorded() {
		if strings.Contains(request.path, "/versions") || strings.Contains(request.path, "/draft") {
			t.Errorf("create should not touch %s", request.path)
		}
	}
}

func TestUpdatePlaybookChangesOnlyTheNamedMetadata(t *testing.T) {
	fixture := newAuthoringFixture(t)

	update := &playbookUpdate{
		body:           map[string]interface{}{"disabled": true},
		linkedPageRefs: []string{"Engineering/Build"},
	}

	output := capture(t, func() error {
		return runPlaybookUpdate(fixture.api(), playbookBuildID, update)
	})

	body := fixture.bodyOf(t, http.MethodPatch, "/pipelines/"+playbookBuildID)
	if body["disabled"] != true {
		t.Errorf("the disabled flag was not sent: %v", body)
	}
	if _, present := body["name"]; present {
		t.Errorf("an unnamed field must not be restated: %v", body)
	}
	if _, present := body["definition"]; present {
		t.Errorf("metadata updates must never carry a definition: %v", body)
	}

	data := output["data"].(map[string]interface{})
	if data["active_version"] == nil {
		t.Errorf("the active version should be reported as unmoved: %v", data)
	}

	for _, request := range fixture.recorded() {
		if strings.Contains(request.path, "/versions") || strings.Contains(request.path, "/draft") {
			t.Errorf("a metadata update must not touch %s", request.path)
		}
	}
}

func TestArchiveAndRestorePlaybook(t *testing.T) {
	fixture := newAuthoringFixture(t)

	archived := capture(t, func() error {
		return runPlaybookArchive(fixture.api(), playbookBuildID)
	})
	if archived["data"].(map[string]interface{})["archived"] != true {
		t.Errorf("archive should report itself: %v", archived["data"])
	}

	restored := capture(t, func() error {
		return runPlaybookRestore(fixture.api(), playbookArchivedID)
	})
	body := fixture.bodyOf(t, http.MethodPatch, "/pipelines/"+playbookArchivedID)
	if body["archived"] != false {
		t.Errorf("restore sends archived=false, got %v", body)
	}
	if len(body) != 1 {
		t.Errorf("restore must send nothing else: %v", body)
	}
	if restored["data"].(map[string]interface{})["name"] != "Retired Playbook (2)" {
		t.Errorf("the restored name should come from the server: %v", restored["data"])
	}
}

// An archive the server would silently no-op is refused here instead: it
// short-circuits on an already-archived playbook and answers 204 without
// writing, so nothing in the response says the archive did not happen.
func TestArchivePlaybookRefusesAPlaybookThatIsAlreadyArchived(t *testing.T) {
	fixture := newAuthoringFixture(t)

	err := captureError(t, func() error {
		return runPlaybookArchive(fixture.api(), playbookArchivedID)
	})
	if !strings.Contains(err.Error(), "is already archived") {
		t.Errorf("the refusal should name the state it found: %v", err)
	}

	for _, request := range fixture.recorded() {
		if request.method != http.MethodGet {
			t.Errorf("a refused archive must not mutate anything: %s %s", request.method, request.path)
		}
	}
}

// A restore the server would silently no-op is refused here instead: its PATCH
// is idempotent and answers 200 whether or not anything was written.
func TestRestorePlaybookRefusesAPlaybookThatIsNotArchived(t *testing.T) {
	fixture := newAuthoringFixture(t)

	err := captureError(t, func() error {
		return runPlaybookRestore(fixture.api(), playbookBuildID)
	})
	if !strings.Contains(err.Error(), "is not archived, so there is nothing to restore") {
		t.Errorf("the refusal should name the state it found: %v", err)
	}

	for _, request := range fixture.recorded() {
		if request.method != http.MethodGet {
			t.Errorf("a refused restore must not mutate anything: %s %s", request.method, request.path)
		}
	}
}

func TestAuthoringRefusesWrongTypesAndOtherAccountsWithoutMutating(t *testing.T) {
	fixture := newAuthoringFixture(t)

	otherAccountURL := "https://app.wallfacer.ai/accounts/" + otherAccountID + "/handbook/" + playbookBuildID
	definition := map[string]interface{}{"steps": []interface{}{}}

	cases := map[string]func() error{
		"page id to publish":    func() error { return runPlaybookPublish(fixture.api(), pageBuildID, "", true) },
		"page id to save-draft": func() error { return runPlaybookSaveDraft(fixture.api(), pageBuildID, definition) },
		"page id to discard":    func() error { return runPlaybookDiscardDraft(fixture.api(), pageBuildID) },
		"page id to archive":    func() error { return runPlaybookArchive(fixture.api(), pageBuildID) },
		"other account publish": func() error { return runPlaybookPublish(fixture.api(), otherAccountURL, "", true) },
		"other account save":    func() error { return runPlaybookSaveDraft(fixture.api(), otherAccountURL, definition) },
		"playbook as create page": func() error {
			return runPlaybookCreate(fixture.api(), "Name", "", playbookBuildID, nil, definition, false)
		},
	}

	for name, run := range cases {
		if err := run(); err == nil {
			t.Errorf("%s: expected a refusal", name)
		}
	}

	for _, request := range fixture.recorded() {
		if request.method != http.MethodGet {
			t.Errorf("a refused authoring call must not %s %s", request.method, request.path)
		}
	}
}

func TestAuthoringNeverCreatesATask(t *testing.T) {
	fixture := newAuthoringFixture(t)

	definition := map[string]interface{}{"steps": []interface{}{}}
	commands := []func() error{
		func() error { return runPlaybookSaveDraft(fixture.api(), playbookBuildID, definition) },
		func() error { return runPlaybookDiffDraft(fixture.api(), playbookBuildID) },
		func() error { return runPlaybookPublish(fixture.api(), playbookBuildID, "", true) },
		func() error { return runPlaybookDiscardDraft(fixture.api(), playbookBuildID) },
		func() error { return runPlaybookDiff(fixture.api(), playbookBuildID, "2", "3") },
		func() error { return runPlaybookArchive(fixture.api(), playbookBuildID) },
		func() error { return runPlaybookRestore(fixture.api(), playbookArchivedID) },
		func() error {
			return runPlaybookCreate(fixture.api(), "New Playbook", "", "", nil, definition, false)
		},
		func() error {
			return runPlaybookUpdate(fixture.api(), playbookBuildID, &playbookUpdate{body: map[string]interface{}{"name": "Renamed"}})
		},
	}

	for _, command := range commands {
		capture(t, command)
	}

	for _, request := range fixture.recorded() {
		if strings.Contains(request.path, "/tasks") {
			t.Errorf("authoring must never touch %s", request.path)
		}
	}
}

func TestCreateWithDraftSavesTheDraftToo(t *testing.T) {
	fixture := newAuthoringFixture(t)

	definition := map[string]interface{}{"steps": []interface{}{}}
	capture(t, func() error {
		return runPlaybookCreate(fixture.api(), "New Playbook", "", "", nil, definition, true)
	})

	if got := fixture.methodsFor("/draft"); len(got) != 1 || !strings.HasPrefix(got[0], http.MethodPut) {
		t.Errorf("--draft should save exactly one draft, got %v", got)
	}
}

func TestLoadDefinitionAcceptsJSONYAMLAndTheRequestEnvelope(t *testing.T) {
	dir := t.TempDir()

	cases := map[string]string{
		"definition.json": `{"format_version":1,"steps":[{"id":"implement","kind":"ai"}]}`,
		"envelope.json":   `{"definition":{"format_version":1,"steps":[{"id":"implement","kind":"ai"}]}}`,
		"definition.yaml": "format_version: 1\nsteps:\n  - id: implement\n    kind: ai\n",
		// What `handbook version` prints: the version record under the
		// CLI's own envelope.
		"printed-version.json": `{"data":{"id":"v","version":2,"definition":{"format_version":1,"steps":[{"id":"implement","kind":"ai"}]},"created_at":"2026-09-18T00:00:00Z"},"reference":{"id":"p"},"follow_up":{"versions":"wallfacer handbook versions p"}}`,
		// What `handbook draft` prints.
		"printed-draft.json": `{"data":{"present":true,"draft":{"definition":{"format_version":1,"steps":[{"id":"implement","kind":"ai"}]},"updated_at":"2026-09-18T00:00:00Z"}},"reference":{"id":"p"}}`,
	}

	for name, content := range cases {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}

		definition, err := loadDefinition(path)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		steps, ok := definition["steps"].([]interface{})
		if !ok || len(steps) != 1 {
			t.Errorf("%s: steps did not survive the parse: %v", name, definition)
			continue
		}
		if steps[0].(map[string]interface{})["id"] != "implement" {
			t.Errorf("%s: unexpected step: %v", name, steps[0])
		}
	}

	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, []byte("  \n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDefinition(empty); err == nil || !strings.Contains(err.Error(), "no definition supplied") {
		t.Errorf("an empty definition should be refused: %v", err)
	}

	notAnObject := filepath.Join(dir, "list.json")
	if err := os.WriteFile(notAnObject, []byte(`["steps"]`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDefinition(notAnObject); err == nil || !strings.Contains(err.Error(), "must be a JSON or YAML object") {
		t.Errorf("a non-object definition should be refused: %v", err)
	}
}

const playbookPublishedOnlyRecordJSON = `{"id":"` + playbookPublishedOnlyID + `","account_id":"` + testAccountID + `","name":"Published Only","description":null,"active_version":{"id":"` + playbookVersionID + `","version":2},"version_count":2,"draft":null,"parent_page_id":null,"position":3,"linked_page_ids":[],"disabled_at":null,"archived_at":null,"created_at":"2026-08-01T00:00:00.000000Z","created_by":1}`

const playbookUnpublishedRecordJSON = `{"id":"` + playbookUnpublishedID + `","account_id":"` + testAccountID + `","name":"Not Published Yet","description":null,"active_version":null,"version_count":0,"draft":{"definition":{"format_version":1,"steps":[{"id":"implement","kind":"ai","title":"Implement the issue"}],"triggers":[]},"updated_at":"2026-09-12T00:00:00.000000Z","updated_by":2},"parent_page_id":null,"position":4,"linked_page_ids":[],"disabled_at":null,"archived_at":null,"created_at":"2026-08-01T00:00:00.000000Z","created_by":1}`

const playbookDisabledRecordJSON = `{"id":"` + playbookDisabledID + `","account_id":"` + testAccountID + `","name":"Paused Playbook","description":null,"active_version":{"id":"` + playbookVersionID + `","version":2},"version_count":2,"draft":{"definition":{"format_version":1,"steps":[{"id":"implement","kind":"ai","title":"Implement the issue, revised"}],"triggers":[]},"updated_at":"2026-09-12T00:00:00.000000Z","updated_by":2},"parent_page_id":null,"position":5,"linked_page_ids":[],"disabled_at":"2026-09-01T00:00:00.000000Z","archived_at":null,"created_at":"2026-08-01T00:00:00.000000Z","created_by":1}`

// The server disambiguates the name on restore and leaves the playbook
// disabled, so a restore answers with more than the archived record and a
// cleared date.
const playbookRestoredRecordJSON = `{"id":"` + playbookArchivedID + `","account_id":"` + testAccountID + `","name":"Retired Playbook (2)","description":null,"active_version":{"id":"` + playbookVersionID + `","version":1},"version_count":1,"draft":null,"parent_page_id":null,"position":9,"linked_page_ids":[],"disabled_at":"2026-08-20T00:00:00.000000Z","archived_at":null,"created_at":"2026-08-01T00:00:00.000000Z","created_by":1}`

const playbookInvalidDraftRecordJSON = `{"id":"` + playbookInvalidDraftID + `","account_id":"` + testAccountID + `","name":"Verbatim Draft","description":null,"active_version":{"id":"` + playbookVersionID + `","version":2},"version_count":2,"draft":{"definition":"steps: not-an-object","updated_at":"2026-09-12T00:00:00.000000Z","updated_by":2},"parent_page_id":null,"position":6,"linked_page_ids":[],"disabled_at":null,"archived_at":null,"created_at":"2026-08-01T00:00:00.000000Z","created_by":1}`

const playbookRejectedRecordJSON = `{"id":"` + playbookRejectedID + `","account_id":"` + testAccountID + `","name":"Rejected Draft","description":null,"active_version":{"id":"` + playbookVersionID + `","version":2},"version_count":2,"draft":{"definition":{"format_version":"one","steps":[]},"updated_at":"2026-09-12T00:00:00.000000Z","updated_by":2},"parent_page_id":null,"position":7,"linked_page_ids":[],"disabled_at":null,"archived_at":null,"created_at":"2026-08-01T00:00:00.000000Z","created_by":1}`

const playbookCreatedRecordJSON = `{"id":"` + playbookCreatedID + `","account_id":"` + testAccountID + `","name":"New Playbook","description":"A description.","active_version":{"id":"` + newVersionID + `","version":1,"created_at":"2026-09-18T00:00:00.000000Z","steps":[{"id":"implement","title":"Implement the issue","kind":"ai"}]},"version_count":1,"draft":null,"parent_page_id":"` + pageBuildID + `","position":8,"linked_page_ids":["` + pageBuildID + `"],"disabled_at":null,"archived_at":null,"created_at":"2026-09-18T00:00:00.000000Z","created_by":1}`

const newVersionRecordJSON = `{"id":"` + newVersionID + `","pipeline_id":"` + playbookBuildID + `","version":3,"definition":{"format_version":1,"steps":[{"id":"implement","kind":"ai","title":"Implement the issue, revised"}],"triggers":[]},"notes":"Revised the implement step.","created_at":"2026-09-18T00:00:00.000000Z","created_by":1}`

const firstVersionRecordJSON = `{"id":"` + newVersionID + `","pipeline_id":"` + playbookUnpublishedID + `","version":1,"definition":{"format_version":1,"steps":[{"id":"implement","kind":"ai","title":"Implement the issue"}],"triggers":[]},"notes":null,"created_at":"2026-09-18T00:00:00.000000Z","created_by":1}`

const versionDiffJSON = `{"data":{"a":{"version":2},"b":{"version":3},"changes":[{"path":"steps[0].title","from":"Implement the issue","to":"Implement the issue, revised"}]}}`

const definitionRejectedJSON = `{"errors":[{"message":"format_version must be an integer","code":"definition_validation_failed","field":"format_version"}]}`
