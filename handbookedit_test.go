package main

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// The write half of the handbook surface. Every test here asserts on the
// request the command actually issued (method, resource ID, and body) as well
// as on what it printed, because a write that lands on the wrong resource and
// prints the right thing is the failure that matters.

// lastMutation returns the single write the command made, failing the test when
// it made none or more than one.
func lastMutation(t *testing.T, fixture *handbookFixture) recordedRequest {
	t.Helper()
	mutations := fixture.mutations()
	if len(mutations) != 1 {
		t.Fatalf("expected exactly one write, got %d: %+v", len(mutations), mutations)
	}
	return mutations[0]
}

func assertNoMutations(t *testing.T, fixture *handbookFixture) {
	t.Helper()
	if mutations := fixture.mutations(); len(mutations) != 0 {
		t.Fatalf("expected no write to be issued, got %+v", mutations)
	}
}

func TestCreatePageSendsTitleBodyAndResolvedParent(t *testing.T) {
	fixture := newHandbookFixture(t)

	out := capture(t, func() error {
		return runHandbookCreate(fixture.api(), handbookEdit{
			body:      map[string]interface{}{"title": "Writing Great PRs", "body": "Lead with the problem."},
			parentRef: "Engineering",
		})
	})

	request := lastMutation(t, fixture)
	if request.method != http.MethodPost || request.path != "/v1/accounts/"+testAccountID+"/pages" {
		t.Fatalf("create issued %s %s", request.method, request.path)
	}

	body := request.decodedBody(t)
	if body["title"] != "Writing Great PRs" || body["body"] != "Lead with the problem." {
		t.Errorf("create sent %v", body)
	}
	if body["parent_page_id"] != pageEngineeringID {
		t.Errorf("parent_page_id is %v, want the resolved ID of the named parent", body["parent_page_id"])
	}

	reference, _ := out["reference"].(map[string]interface{})
	if reference["id"] != pageCreatedID {
		t.Errorf("result names %v, want the created page", reference["id"])
	}
	if note, _ := out["note"].(string); !strings.Contains(note, "live") {
		t.Errorf("create should say the page is live knowledge, got %q", note)
	}
}

func TestCreateWithoutATitleIsRejectedBeforeWriting(t *testing.T) {
	fixture := newHandbookFixture(t)

	err := runHandbookCreate(fixture.api(), handbookEdit{body: map[string]interface{}{"body": "No title."}})
	if err == nil || !strings.Contains(err.Error(), "a title is required") {
		t.Fatalf("expected a missing-title error, got %v", err)
	}
	assertNoMutations(t, fixture)
}

func TestCreateAtTheTopLevelSendsAnExplicitNullParent(t *testing.T) {
	fixture := newHandbookFixture(t)

	capture(t, func() error {
		return runHandbookCreate(fixture.api(), handbookEdit{
			body:     map[string]interface{}{"title": "Writing Great PRs"},
			topLevel: true,
		})
	})

	body := lastMutation(t, fixture).decodedBody(t)
	parent, present := body["parent_page_id"]
	if !present || parent != nil {
		t.Errorf("parent_page_id is %v (present: %t), want an explicit null", parent, present)
	}
}

func TestUpdateSendsOnlyTheFieldsGivenAndNamesTheRevisionReads(t *testing.T) {
	fixture := newHandbookFixture(t)

	out := capture(t, func() error {
		return runHandbookUpdate(fixture.api(), "Engineering/Build", handbookEdit{
			body: map[string]interface{}{"body": "The rewritten build page."},
		})
	})

	request := lastMutation(t, fixture)
	if request.method != http.MethodPatch || request.path != "/v1/accounts/"+testAccountID+"/pages/"+pageBuildID {
		t.Fatalf("update issued %s %s, want a PATCH of the resolved page", request.method, request.path)
	}

	body := request.decodedBody(t)
	if len(body) != 1 || body["body"] != "The rewritten build page." {
		t.Errorf("update sent %v, want only the field that was given", body)
	}

	followUp, _ := out["follow_up"].(map[string]interface{})
	revisions, _ := followUp["revisions"].(string)
	if !strings.Contains(revisions, pageBuildID) {
		t.Errorf("follow_up.revisions is %q, want the command that reads this page's history", revisions)
	}
	if _, templated := followUp["revision"]; templated {
		t.Errorf("a write holds no revision id, so it must not name a revision command: %v", followUp)
	}
	if note, _ := out["note"].(string); !strings.Contains(note, "revision") {
		t.Errorf("update should say history is retained, got %q", note)
	}
}

func TestUpdateWithNothingToChangeIsRejectedBeforeWriting(t *testing.T) {
	fixture := newHandbookFixture(t)

	err := runHandbookUpdate(fixture.api(), pageBuildID, handbookEdit{})
	if err == nil || !strings.Contains(err.Error(), "nothing to update") {
		t.Fatalf("expected a nothing-to-update error, got %v", err)
	}
	assertNoMutations(t, fixture)
}

func TestUpdateRefusesAPlaybookWithoutWriting(t *testing.T) {
	fixture := newHandbookFixture(t)

	err := runHandbookUpdate(fixture.api(), playbookBuildID, handbookEdit{
		body: map[string]interface{}{"title": "Build"},
	})
	if err == nil || !strings.Contains(err.Error(), "is a playbook, but a page was requested") {
		t.Fatalf("expected a wrong-type error, got %v", err)
	}
	assertNoMutations(t, fixture)
}

func TestWritesForAnotherAccountNeverReachTheServer(t *testing.T) {
	fixture := newHandbookFixture(t)
	reference := "https://app.wallfacer.ai/accounts/" + otherAccountID + "/handbook/pages/" + pageBuildID

	writes := map[string]func() error{
		"update": func() error {
			return runHandbookUpdate(fixture.api(), reference, handbookEdit{body: map[string]interface{}{"title": "x"}})
		},
		"delete":  func() error { return runHandbookDelete(fixture.api(), reference) },
		"restore": func() error { return runHandbookRestore(fixture.api(), reference) },
		"move":    func() error { return runHandbookMove(fixture.api(), reference, "", handbookEdit{topLevel: true}, -1) },
	}

	for name, write := range writes {
		err := write()
		if err == nil || !strings.Contains(err.Error(), otherAccountID) {
			t.Errorf("%s: expected the account mismatch to be refused, got %v", name, err)
		}
	}

	if got := fixture.recorded(); len(got) != 0 {
		t.Errorf("expected the mismatch to be caught before any request, got %+v", got)
	}
}

func TestDeleteReportsReparentedChildrenAndRetainedHistory(t *testing.T) {
	fixture := newHandbookFixture(t)

	out := capture(t, func() error {
		return runHandbookDelete(fixture.api(), "Engineering")
	})

	request := lastMutation(t, fixture)
	if request.method != http.MethodDelete || request.path != "/v1/accounts/"+testAccountID+"/pages/"+pageEngineeringID {
		t.Fatalf("delete issued %s %s", request.method, request.path)
	}

	data, _ := out["data"].(map[string]interface{})
	if data["deleted"] != true || data["revision_history"] != "retained" {
		t.Errorf("delete result is %v", data)
	}
	if data["reparented_to"] != nil {
		t.Errorf("reparented_to is %v, want null for a top-level page's children", data["reparented_to"])
	}

	reparented, _ := data["reparented"].([]interface{})
	if len(reparented) != 3 {
		t.Fatalf("expected the page's 3 children to be named, got %v", reparented)
	}
	types := map[string]bool{}
	for _, item := range reparented {
		child, _ := item.(map[string]interface{})
		types[child["type"].(string)] = true
	}
	if !types[kindPage] || !types[kindPlaybook] {
		t.Errorf("both sub-pages and playbooks move up; got %v", reparented)
	}

	followUp, _ := out["follow_up"].(map[string]interface{})
	if restore, _ := followUp["restore"].(string); !strings.Contains(restore, pageEngineeringID) {
		t.Errorf("follow_up.restore is %q, want the ID-based restore", restore)
	}
}

func TestDeleteRefusesAnAlreadyDeletedPage(t *testing.T) {
	fixture := newHandbookFixture(t)

	err := runHandbookDelete(fixture.api(), pageDeletedID)
	if err == nil || !strings.Contains(err.Error(), "already deleted") {
		t.Fatalf("expected an already-deleted error, got %v", err)
	}
	assertNoMutations(t, fixture)
}

func TestRestoreByIDSendsDeletedFalse(t *testing.T) {
	fixture := newHandbookFixture(t)

	out := capture(t, func() error {
		return runHandbookRestore(fixture.api(), pageDeletedID)
	})

	request := lastMutation(t, fixture)
	if request.method != http.MethodPatch || request.path != "/v1/accounts/"+testAccountID+"/pages/"+pageDeletedID {
		t.Fatalf("restore issued %s %s", request.method, request.path)
	}

	body := request.decodedBody(t)
	if len(body) != 1 || body["deleted"] != false {
		t.Errorf("restore sent %v, want only deleted=false", body)
	}

	data, _ := out["data"].(map[string]interface{})
	if data["deleted_at"] != nil {
		t.Errorf("restored record still carries deleted_at: %v", data["deleted_at"])
	}
	if note, _ := out["note"].(string); !strings.Contains(note, "top level") {
		t.Errorf("restore should say where a page lands when its parent went too, got %q", note)
	}
}

func TestRestoreRefusesALivePage(t *testing.T) {
	fixture := newHandbookFixture(t)

	err := runHandbookRestore(fixture.api(), pageBuildID)
	if err == nil || !strings.Contains(err.Error(), "not deleted") {
		t.Fatalf("expected a not-deleted error, got %v", err)
	}
	assertNoMutations(t, fixture)
}

// Restore takes the same reference forms as every other command; the guard is
// on state, not on how the target was named. A name only ever resolves to an
// active page, so it lands on the same refusal.
func TestRestoreResolvesANameAndRefusesItOnState(t *testing.T) {
	fixture := newHandbookFixture(t)

	err := runHandbookRestore(fixture.api(), "Engineering/Build")
	if err == nil || !strings.Contains(err.Error(), "not deleted") {
		t.Fatalf("expected a not-deleted error, got %v", err)
	}
	assertNoMutations(t, fixture)
}

func TestMovePageUnderAnotherPage(t *testing.T) {
	fixture := newHandbookFixture(t)

	capture(t, func() error {
		return runHandbookMove(fixture.api(), "Engineering/Build", kindPage, handbookEdit{parentRef: "Product"}, 0)
	})

	request := lastMutation(t, fixture)
	if request.method != http.MethodPatch || request.path != "/v1/accounts/"+testAccountID+"/pages/"+pageBuildID {
		t.Fatalf("move issued %s %s", request.method, request.path)
	}

	body := request.decodedBody(t)
	if body["parent_page_id"] != pageProductID || body["position"] != float64(0) {
		t.Errorf("move sent %v", body)
	}
}

func TestMovePlaybookChangesHierarchyAndNothingElse(t *testing.T) {
	fixture := newHandbookFixture(t)

	out := capture(t, func() error {
		return runHandbookMove(fixture.api(), playbookBuildID, kindPlaybook, handbookEdit{topLevel: true}, 2)
	})

	request := lastMutation(t, fixture)
	if request.method != http.MethodPatch || request.path != "/v1/accounts/"+testAccountID+"/pipelines/"+playbookBuildID {
		t.Fatalf("move issued %s %s, want a PATCH of the pipeline", request.method, request.path)
	}

	body := request.decodedBody(t)
	if len(body) != 2 {
		t.Fatalf("move sent %v, want the parent and position only", body)
	}
	if parent, present := body["parent_page_id"]; !present || parent != nil {
		t.Errorf("parent_page_id is %v, want an explicit null", parent)
	}
	if body["position"] != float64(2) {
		t.Errorf("position is %v, want 2", body["position"])
	}
	for _, field := range []string{"definition", "draft", "triggers", "disabled", "archived", "version"} {
		if _, present := body[field]; present {
			t.Errorf("move must not send %s", field)
		}
	}

	for _, request := range fixture.recorded() {
		if strings.Contains(request.path, "/tasks") {
			t.Errorf("move must never touch tasks, got %s %s", request.method, request.path)
		}
		if request.method != http.MethodGet && strings.Contains(request.path, "/versions") {
			t.Errorf("move must not publish, got %s %s", request.method, request.path)
		}
	}

	if note, _ := out["note"].(string); !strings.Contains(note, "no task") {
		t.Errorf("the playbook move should say no task was created, got %q", note)
	}
}

func TestMoveReachesAnArchivedPlaybookByID(t *testing.T) {
	fixture := newHandbookFixture(t)

	capture(t, func() error {
		return runHandbookMove(fixture.api(), playbookArchivedID, "", handbookEdit{parentRef: "Product"}, -1)
	})

	request := lastMutation(t, fixture)
	if request.path != "/v1/accounts/"+testAccountID+"/pipelines/"+playbookArchivedID {
		t.Fatalf("move issued %s %s, want the archived playbook", request.method, request.path)
	}
	if body := request.decodedBody(t); len(body) != 1 || body["parent_page_id"] != pageProductID {
		t.Errorf("move sent %v, want the parent alone when no position was given", body)
	}
}

func TestMoveSurfacesAServerRejectedCycle(t *testing.T) {
	fixture := newHandbookFixture(t)
	fixture.onWrite(func(r *http.Request, body string) (string, int, bool) {
		return `{"errors":[{"message":"A page cannot be moved under itself or one of its own sub-pages.","code":"page_cycle"}]}`, http.StatusUnprocessableEntity, true
	})

	err := runHandbookMove(fixture.api(), "Engineering", "", handbookEdit{parentRef: "Engineering/Build"}, -1)
	if err == nil {
		t.Fatal("expected the server's cycle rejection to be an error, not a result")
	}
	if !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), "page_cycle") {
		t.Errorf("error should carry the server's status and message, got %v", err)
	}
}

func TestReorderSendsTheCompleteOrderedListForOneParent(t *testing.T) {
	fixture := newHandbookFixture(t)

	out := capture(t, func() error {
		return runHandbookReorder(fixture.api(), "Engineering", false, []string{pageReviewEngID, playbookBuildID, pageBuildID})
	})

	request := lastMutation(t, fixture)
	if request.method != http.MethodPatch || request.path != "/v1/accounts/"+testAccountID+"/handbook" {
		t.Fatalf("reorder issued %s %s, want the atomic handbook reorder", request.method, request.path)
	}

	body := request.decodedBody(t)
	if body["parent_page_id"] != pageEngineeringID {
		t.Errorf("parent_page_id is %v", body["parent_page_id"])
	}

	children, _ := body["children"].([]interface{})
	want := []struct{ kind, id string }{
		{kindPage, pageReviewEngID},
		{kindPlaybook, playbookBuildID},
		{kindPage, pageBuildID},
	}
	if len(children) != len(want) {
		t.Fatalf("sent %d children, want %d: %v", len(children), len(want), children)
	}
	for i, expected := range want {
		child, _ := children[i].(map[string]interface{})
		if child["type"] != expected.kind || child["id"] != expected.id {
			t.Errorf("child %d is %v, want %s %s", i, child, expected.kind, expected.id)
		}
	}

	if _, ok := out["data"].(map[string]interface{})["tree"]; !ok {
		t.Errorf("reorder should return the updated tree, got %v", out["data"])
	}
}

func TestReorderTheTopLevel(t *testing.T) {
	fixture := newHandbookFixture(t)

	capture(t, func() error {
		return runHandbookReorder(fixture.api(), "", true, []string{"Product", "Engineering"})
	})

	body := lastMutation(t, fixture).decodedBody(t)
	if parent, present := body["parent_page_id"]; !present || parent != nil {
		t.Errorf("parent_page_id is %v, want an explicit null for the top level", parent)
	}

	children, _ := body["children"].([]interface{})
	if len(children) != 2 {
		t.Fatalf("sent %v, want both top-level pages", children)
	}
	if first, _ := children[0].(map[string]interface{}); first["id"] != pageProductID {
		t.Errorf("first child is %v, want the page the caller put first", first)
	}
}

func TestReorderRejectsAnIncompleteOrDuplicatedList(t *testing.T) {
	fixture := newHandbookFixture(t)

	cases := map[string]struct {
		children []string
		want     string
	}{
		"omits a sibling": {
			children: []string{pageBuildID, playbookBuildID},
			want:     "missing: page " + pageReviewEngID,
		},
		"duplicates a sibling": {
			children: []string{pageBuildID, pageBuildID, playbookBuildID, pageReviewEngID},
			want:     "twice",
		},
		"names another parent's child": {
			children: []string{pageBuildID, playbookBuildID, pageReviewEngID, pageReviewProdID},
			want:     "is not a child of page " + pageEngineeringID,
		},
	}

	for name, tc := range cases {
		err := runHandbookReorder(fixture.api(), "Engineering", false, tc.children)
		if err == nil {
			t.Errorf("%s: expected the list to be rejected", name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not contain %q", name, err.Error(), tc.want)
		}
	}

	assertNoMutations(t, fixture)
}

func TestWriteCommandsNeverCreateTasks(t *testing.T) {
	fixture := newHandbookFixture(t)

	commands := []func() error{
		func() error {
			return runHandbookCreate(fixture.api(), handbookEdit{body: map[string]interface{}{"title": "Writing Great PRs"}})
		},
		func() error {
			return runHandbookUpdate(fixture.api(), pageBuildID, handbookEdit{body: map[string]interface{}{"title": "Build"}})
		},
		func() error {
			return runHandbookMove(fixture.api(), playbookBuildID, "", handbookEdit{topLevel: true}, -1)
		},
		func() error {
			return runHandbookReorder(fixture.api(), "Engineering", false, []string{pageBuildID, playbookBuildID, pageReviewEngID})
		},
		func() error { return runHandbookDelete(fixture.api(), pageBuildID) },
		func() error { return runHandbookRestore(fixture.api(), pageDeletedID) },
	}

	for _, command := range commands {
		capture(t, command)
	}

	for _, request := range fixture.recorded() {
		if strings.Contains(request.path, "/tasks") {
			t.Errorf("%s %s: handbook writes must never create a task", request.method, request.path)
		}
	}
	if len(fixture.mutations()) != len(commands) {
		t.Errorf("expected one write per command, got %d", len(fixture.mutations()))
	}
}

// withStdin runs fn with a piped stdin holding contents, which is how
// cli.GetBody sees a request body.
func withStdin(t *testing.T, contents string, fn func()) {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	go func() {
		writer.WriteString(contents)
		writer.Close()
	}()

	previous := os.Stdin
	os.Stdin = reader
	defer func() {
		os.Stdin = previous
		reader.Close()
	}()

	fn()
}

func TestARequestBodyOfNullIsRejected(t *testing.T) {
	cmd := handbookCreateCommand(testAccountID)

	withStdin(t, "null", func() {
		if _, err := handbookEditFromFlags(cmd); err == nil {
			t.Fatal("expected a literal null body to be rejected, not to become an empty edit")
		}
	})
}

// withOpenStdin runs fn with stdin a pipe nobody ever writes to or closes,
// which is what a CI step, a wrapper script, or an agent harness hands the
// command. Reading it to the end never returns.
func withOpenStdin(t *testing.T, fn func()) {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	previous := os.Stdin
	os.Stdin = reader
	defer func() {
		os.Stdin = previous
		writer.Close()
		reader.Close()
	}()

	fn()
}

func TestABodyFlagIsNotReadFromStdin(t *testing.T) {
	cases := map[string]struct {
		cmd   *cobra.Command
		flags map[string]string
	}{
		"create --body":       {handbookCreateCommand(testAccountID), map[string]string{"body": "y"}},
		"update --body":       {handbookUpdateCommand(testAccountID), map[string]string{"body": "y"}},
		"update --clear-body": {handbookUpdateCommand(testAccountID), map[string]string{"clear-body": "true"}},
	}

	for name, tc := range cases {
		for flag, value := range tc.flags {
			if err := tc.cmd.Flags().Set(flag, value); err != nil {
				t.Fatalf("%s: setting --%s: %v", name, flag, err)
			}
		}

		done := make(chan handbookEdit, 1)
		withOpenStdin(t, func() {
			go func() {
				edit, err := handbookEditFromFlags(tc.cmd)
				if err != nil {
					t.Errorf("%s: %v", name, err)
					return
				}
				done <- edit
			}()

			select {
			case edit := <-done:
				if _, present := edit.body["body"]; !present {
					t.Errorf("%s: built %v, want the body the flag supplied", name, edit.body)
				}
			case <-time.After(5 * time.Second):
				t.Errorf("%s: blocked on stdin though the body came from a flag", name)
			}
		})
	}
}

func TestMoveReportsThePathTheEntryNowHas(t *testing.T) {
	fixture := newHandbookFixture(t)
	moved := strings.Replace(pageBuildRecordJSON, `"parent_page_id":"`+pageEngineeringID+`"`, `"parent_page_id":"`+pageProductID+`"`, 1)
	fixture.onWrite(func(r *http.Request, body string) (string, int, bool) {
		if r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/pages/"+pageBuildID) {
			return wrapData(moved), http.StatusOK, true
		}
		return "", 0, false
	})

	output := capture(t, func() error {
		return runHandbookMove(fixture.api(), "Engineering/Build", kindPage, handbookEdit{parentRef: "Product"}, 0)
	})

	reference := output["reference"].(map[string]interface{})
	if reference["path"] != "Product/Build" {
		t.Errorf("move reported path %v, want the post-move path Product/Build", reference["path"])
	}
}

func TestUpdateToTheTopLevelReportsTheNewPath(t *testing.T) {
	fixture := newHandbookFixture(t)
	promoted := strings.Replace(pageBuildRecordJSON, `"parent_page_id":"`+pageEngineeringID+`"`, `"parent_page_id":null`, 1)
	fixture.onWrite(func(r *http.Request, body string) (string, int, bool) {
		if r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/pages/"+pageBuildID) {
			return wrapData(promoted), http.StatusOK, true
		}
		return "", 0, false
	})

	output := capture(t, func() error {
		return runHandbookUpdate(fixture.api(), "Engineering/Build", handbookEdit{topLevel: true})
	})

	reference := output["reference"].(map[string]interface{})
	if reference["path"] != "Build" {
		t.Errorf("update reported path %v, want the post-move path Build", reference["path"])
	}
}

func TestDeleteReportsNoDestinationWhenNothingWasReparented(t *testing.T) {
	fixture := newHandbookFixture(t)

	out := capture(t, func() error {
		return runHandbookDelete(fixture.api(), "Engineering/Build")
	})

	data, _ := out["data"].(map[string]interface{})
	reparented, _ := data["reparented"].([]interface{})
	if len(reparented) != 0 {
		t.Fatalf("expected a childless page, got %v", reparented)
	}
	if data["reparented_to"] != nil {
		t.Errorf("reparented_to is %v, want null when nothing moved", data["reparented_to"])
	}
	if note, _ := out["note"].(string); !strings.Contains(note, "nothing moved") {
		t.Errorf("note is %q, want it to say nothing was filed under the page", note)
	}
}

func TestANegativePositionIsRefusedBeforeTheMove(t *testing.T) {
	cmd := handbookMoveCommand(testAccountID)
	if err := cmd.Flags().Set("position", "-5"); err != nil {
		t.Fatalf("setting --position: %v", err)
	}

	position, err := handbookPositionFlag(cmd)
	if err == nil {
		t.Fatalf("a negative position resolved to %d instead of being refused", position)
	}
	if !strings.Contains(err.Error(), "--position") {
		t.Errorf("the refusal should name the flag: %v", err)
	}
}

func TestAnUnsetPositionLeavesTheEntryWhereItIs(t *testing.T) {
	position, err := handbookPositionFlag(handbookMoveCommand(testAccountID))
	if err != nil {
		t.Fatalf("no --position: %v", err)
	}
	if position != -1 {
		t.Errorf("position is %d, want the -1 sentinel that omits the field", position)
	}
}

// TestPositionIsDocumentedAsAnInsertPointEverywhere guards the mismatch behind
// #120: the server inserts and renormalizes on create exactly as it does on
// update and move, so no command may describe --position as a sort key.
func TestPositionIsDocumentedAsAnInsertPointEverywhere(t *testing.T) {
	commands := map[string]*cobra.Command{
		"create": handbookCreateCommand(testAccountID),
		"update": handbookUpdateCommand(testAccountID),
		"move":   handbookMoveCommand(testAccountID),
	}
	for name, cmd := range commands {
		flag := cmd.Flags().Lookup("position")
		if flag == nil {
			t.Fatalf("%s has no --position flag", name)
		}
		if flag.Usage != handbookPositionInsertHelp {
			t.Errorf("%s describes --position as %q, want the shared insert help", name, flag.Usage)
		}
		if strings.Contains(strings.ToLower(cmd.Long), "verbatim") {
			t.Errorf("%s's long description still calls a position verbatim: %s", name, cmd.Long)
		}
	}
}
