package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/WallfacerTech/openapi-cli-generator/cli"
	"github.com/spf13/viper"
)

// followRun drives the generated read operation, so this exercises it against a
// stub of the API: the cursor each read sends is what keeps a follow from
// reprinting output it has already shown.
func TestFollowRunReadsFromTheCursorUntilTheRunEnds(t *testing.T) {
	cli.Init(&cli.Config{AppName: "wallfacer", EnvPrefix: "WALLFACER"})

	bodies := []string{
		`{"data":{"id":"019f8c31","status":"running","exit_code":null,"output":{"text":"first\n","offset":0,"next_offset":6,"truncated":false}}}`,
		`{"data":{"id":"019f8c31","status":"running","exit_code":null,"output":null}}`,
		`{"data":{"id":"019f8c31","status":"failed","exit_code":2,"duration_ms":1200,"output":{"text":"second\n","offset":6,"next_offset":13,"truncated":false}}}`,
	}

	var sinces []string
	reads := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want := "/v1/accounts/acct/vms/vm/runs/run"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
		}
		sinces = append(sinces, r.URL.Query().Get("since"))

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(bodies[reads]))
		reads++
	}))
	defer server.Close()

	viper.Set("server", server.URL)

	var out, errOut bytes.Buffer
	run, err := followRun("acct", "vm", "run", 0, 0, 0, &out, &errOut)
	if err != nil {
		t.Fatalf("followRun: %v", err)
	}

	if reads != len(bodies) {
		t.Errorf("made %d reads, want %d", reads, len(bodies))
	}
	// First read starts at the beginning (no cursor), the second continues
	// from the first slice, and the third holds there because the second
	// read returned no output.
	if want := []string{"", "6", "6"}; !equalStrings(sinces, want) {
		t.Errorf("since = %v, want %v", sinces, want)
	}
	if out.String() != "first\nsecond\n" {
		t.Errorf("stdout = %q, want the two slices in order", out.String())
	}
	if run.Status != "failed" || run.ExitCode == nil || *run.ExitCode != 2 {
		t.Errorf("run = %+v, want the terminal record", run)
	}
	if got := runExitCode(run); got != 2 {
		t.Errorf("runExitCode() = %d, want 2", got)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
