package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func TestInjectedAccountPreservesGeneratedArgumentValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		path string
	}{
		{"todo missing agent", []string{"todos", "get", "todo"}, ""},
		{"todo missing both", []string{"todos", "get"}, ""},
		{"agent missing id", []string{"agents", "get"}, ""},
		{"todo complete", []string{"todos", "get", "agent", "todo"}, "/v1/accounts/acct/agents/agent/todos/todo"},
		{"account only", []string{"agents", "list"}, "/v1/accounts/acct/agents"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"data":[]}`))
			}))
			defer server.Close()
			viper.Set("server", server.URL)
			viper.Set("output-format", "json")
			viper.Set("query", "")
			defer func() { viper.Set("server", ""); viper.Set("output-format", "") }()
			previousRoot, previousOut := cli.Root, cli.Stdout
			defer func() { cli.Root, cli.Stdout = previousRoot, previousOut }()
			var output bytes.Buffer
			cli.Stdout = &output
			cli.Root = &cobra.Command{Use: "wallfacer", SilenceErrors: true, SilenceUsage: true}
			registerGenerated(cli.Root, func() { openapiRegister(false) })
			injectAccountID(cli.Root, "acct")
			cli.Root.SetArgs(tc.args)
			err := cli.Root.Execute()
			if tc.path == "" {
				if err == nil {
					t.Fatal("missing arguments accepted")
				}
				if !strings.Contains(err.Error(), "Usage:") || !strings.Contains(err.Error(), "agent-user-id") {
					t.Errorf("missing visible usage: %v", err)
				}
				if len(paths) != 0 {
					t.Fatalf("invalid command sent requests: %v", paths)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if len(paths) != 1 || paths[0] != tc.path {
					t.Fatalf("requests=%v, want %s", paths, tc.path)
				}
				if !strings.Contains(output.String(), "data") {
					t.Fatalf("response not printed: %s", &output)
				}
			}
		})
	}
}
