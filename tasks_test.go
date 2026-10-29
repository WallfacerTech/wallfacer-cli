package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// newGeneratedRoot registers the real generated commands on a fresh root, with
// the task listing's paging added the way main adds it, against a server that
// records each query string it is sent.
func newGeneratedRoot(t *testing.T) (*cobra.Command, *[]url.Values) {
	t.Helper()
	var queries []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/accounts/acct/tasks") {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		queries = append(queries, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"meta":{"per_page":2,"next_cursor":"next"}}`))
	}))
	t.Cleanup(server.Close)
	viper.Set("server", server.URL)
	t.Cleanup(func() { viper.Set("server", "") })

	previous := cli.Root
	cli.Root = &cobra.Command{Use: "wallfacer"}
	t.Cleanup(func() { cli.Root = previous })
	registerGenerated(cli.Root, func() { openapiRegister(false) })
	registerAccountTaskListPaging(cli.Root)
	return cli.Root, &queries
}

func runRoot(t *testing.T, root *cobra.Command, args ...string) {
	t.Helper()
	viper.Set("output-format", "json")
	viper.Set("query", "")
	defer viper.Set("output-format", "")

	var buf bytes.Buffer
	previous := cli.Stdout
	cli.Stdout = &buf
	defer func() { cli.Stdout = previous }()

	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("%v: %v", args, err)
	}
}

func TestTaskListSearchComposesWithFiltersAndPaging(t *testing.T) {
	root, queries := newGeneratedRoot(t)

	runRoot(t, root, "tasks", "account-list", "acct", "--q", "login redirect", "--status", "done", "--owner-user-id", "7", "--per-page", "2", "--cursor", "abc")

	if len(*queries) != 1 {
		t.Fatalf("want one request, got %d", len(*queries))
	}
	got := (*queries)[0]
	want := map[string]string{"q": "login redirect", "status": "done", "owner_user_id": "7", "per_page": "2", "cursor": "abc"}
	for key, value := range want {
		if got[key] == nil || len(got[key]) != 1 || got[key][0] != value {
			t.Errorf("query %s = %v, want [%s] (full query %v)", key, got[key], value, got)
		}
	}
}

func TestTaskListSendsNoSearchOrCursorUnlessAsked(t *testing.T) {
	root, queries := newGeneratedRoot(t)

	runRoot(t, root, "tasks", "account-list", "acct", "--status", "active")

	got := (*queries)[0]
	if got.Has("q") || got.Has("cursor") {
		t.Errorf("an unfiltered listing should send neither q nor cursor: %v", got)
	}
	if got.Get("status") != "active" {
		t.Errorf("status = %q, want active", got.Get("status"))
	}
}

func TestCrossAccountTaskListUsesNewRouteAndFilters(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"meta":{"next_cursor":null}}`))
	}))
	defer server.Close()
	viper.Set("server", server.URL)
	defer viper.Set("server", "")

	previous := cli.Root
	cli.Root = &cobra.Command{Use: "wallfacer"}
	defer func() { cli.Root = previous }()
	registerGenerated(cli.Root, func() { openapiRegister(false) })
	registerAccountTaskListPaging(cli.Root)

	runRoot(t, cli.Root, "tasks", "list", "--scope", "visible", "--account-id", "acct", "--q", "login", "--per-page", "2", "--cursor", "next")

	if gotPath != "/v1/tasks" {
		t.Errorf("path = %q, want /v1/tasks", gotPath)
	}
	for key, want := range map[string]string{"scope": "visible", "account_id": "acct", "q": "login", "per_page": "2", "cursor": "next"} {
		if got := gotQuery.Get(key); got != want {
			t.Errorf("query %s = %q, want %q", key, got, want)
		}
	}
}
