package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func decodeBody(t *testing.T, body string) map[string]interface{} {
	t.Helper()

	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("bad test fixture: %v", err)
	}

	return decoded
}

func TestDecodeRunReadsTheRecordAndOutputSlice(t *testing.T) {
	run, err := decodeRun(decodeBody(t, `{"data":{"id":"019f8c31","status":"completed","exit_code":0,"error":null,"duration_ms":3184,"output":{"text":"ok\n","offset":0,"next_offset":3,"truncated":false}}}`))
	if err != nil {
		t.Fatalf("decodeRun: %v", err)
	}

	if run.ID != "019f8c31" || run.Status != "completed" {
		t.Errorf("got id %q status %q", run.ID, run.Status)
	}
	if run.ExitCode == nil || *run.ExitCode != 0 {
		t.Errorf("got exit code %v, want 0", run.ExitCode)
	}
	if run.Output == nil || run.Output.NextOffset != 3 {
		t.Errorf("got output %+v, want next_offset 3", run.Output)
	}
}

func TestDecodeRunRejectsResponsesWithoutARun(t *testing.T) {
	for name, body := range map[string]string{
		"no data key": `{"errors":[{"message":"nope"}]}`,
		"no id":       `{"data":{"status":"running"}}`,
	} {
		if _, err := decodeRun(decodeBody(t, body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestTerminalTracksInFlightStatuses(t *testing.T) {
	cases := map[string]bool{
		"pending":   false,
		"running":   false,
		"completed": true,
		"failed":    true,
		"canceled":  true,
		"timed_out": true,
		// A status this CLI does not know ends a follow rather than
		// waiting on it until the run's deadline.
		"quarantined": true,
	}

	for status, want := range cases {
		run := &commandRun{Status: status}
		if got := run.terminal(); got != want {
			t.Errorf("%s: terminal() = %v, want %v", status, got, want)
		}
	}
}

func TestWriteRunOutputAdvancesTheCursor(t *testing.T) {
	var out, errOut bytes.Buffer

	run := &commandRun{Output: &runOutput{Text: "PHPUnit\n", Offset: 512, NextOffset: 520}}
	cursor := writeRunOutput(run, 512, &out, &errOut)

	if cursor != 520 {
		t.Errorf("cursor = %d, want 520", cursor)
	}
	if out.String() != "PHPUnit\n" {
		t.Errorf("stdout = %q", out.String())
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q, want nothing", errOut.String())
	}
}

func TestWriteRunOutputHoldsTheCursorWhenAReadReturnsNothing(t *testing.T) {
	var out, errOut bytes.Buffer

	// A read can answer with the record and no output at all: the VM was
	// unreachable, or the run's output has already been dropped.
	if cursor := writeRunOutput(&commandRun{}, 4096, &out, &errOut); cursor != 4096 {
		t.Errorf("cursor = %d, want 4096", cursor)
	}

	// An empty slice at the same offset must not rewind the cursor either.
	run := &commandRun{Output: &runOutput{Offset: 4096, NextOffset: 4096}}
	if cursor := writeRunOutput(run, 4096, &out, &errOut); cursor != 4096 {
		t.Errorf("cursor = %d, want 4096", cursor)
	}
	if out.Len() != 0 {
		t.Errorf("stdout = %q, want nothing", out.String())
	}
}

func TestWriteRunOutputWarnsOnDroppedOutput(t *testing.T) {
	var out, errOut bytes.Buffer

	run := &commandRun{Output: &runOutput{Text: "…later\n", Offset: 8192, NextOffset: 8200, Truncated: true}}
	writeRunOutput(run, 0, &out, &errOut)

	if !strings.Contains(errOut.String(), "8192") {
		t.Errorf("stderr = %q, want a warning naming the offset", errOut.String())
	}
	if out.String() != "…later\n" {
		t.Errorf("stdout = %q, want the slice printed anyway", out.String())
	}
}

func TestRunExitCode(t *testing.T) {
	code := 3

	cases := []struct {
		name string
		run  *commandRun
		want int
	}{
		{"the command's own code wins", &commandRun{Status: "failed", ExitCode: &code}, 3},
		{"completed without a code", &commandRun{Status: "completed"}, 0},
		{"canceled", &commandRun{Status: "canceled"}, exitRunCanceled},
		{"timed out", &commandRun{Status: "timed_out"}, exitRunTimedOut},
		{"never started", &commandRun{Status: "failed", Error: "agent unreachable"}, exitRunUnknown},
	}

	for _, c := range cases {
		if got := runExitCode(c.run); got != c.want {
			t.Errorf("%s: runExitCode() = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestRunSummary(t *testing.T) {
	code := 1
	duration := int64(62110)

	summary := runSummary(&commandRun{ID: "019f8c31", Status: "failed", ExitCode: &code, DurationMs: &duration})
	for _, want := range []string{"019f8c31", "failed", "exit 1", "1m2.11s"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q is missing %q", summary, want)
		}
	}

	summary = runSummary(&commandRun{ID: "019f8c31", Status: "failed", Error: "agent unreachable"})
	if !strings.Contains(summary, "agent unreachable") {
		t.Errorf("summary %q is missing the error", summary)
	}
}

func TestParseRunEnv(t *testing.T) {
	env, err := parseRunEnv([]string{"CI=true", "DEBUG=", "TOKEN=a=b"})
	if err != nil {
		t.Fatalf("parseRunEnv: %v", err)
	}

	want := map[string]string{"CI": "true", "DEBUG": "", "TOKEN": "a=b"}
	for key, value := range want {
		if env[key] != value {
			t.Errorf("env[%q] = %q, want %q", key, env[key], value)
		}
	}

	for _, bad := range []string{"CI", "=true"} {
		if _, err := parseRunEnv([]string{bad}); err == nil {
			t.Errorf("parseRunEnv(%q): expected an error", bad)
		}
	}
}
