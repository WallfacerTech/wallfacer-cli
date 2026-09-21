package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// hardenedTree is the shape the real CLI has: a root with groups under it, a
// group with leaf commands under that, and a leaf that takes flags. It is
// built per test because hardenUnknownArgs mutates the commands it walks.
func hardenedTree() *cobra.Command {
	root := &cobra.Command{Use: "wallfacer"}

	handbook := &cobra.Command{Use: "handbook", Short: "Read the handbook"}
	list := &cobra.Command{
		Use: "list",
		Run: func(cmd *cobra.Command, args []string) {},
	}
	list.Flags().String("type", "", "Entry type")
	handbook.AddCommand(list)

	root.AddCommand(handbook)
	root.AddCommand(&cobra.Command{
		Use: "run",
		Run: func(cmd *cobra.Command, args []string) {},
	})

	hardenUnknownArgs(root)
	return root
}

// execute runs the tree with the given arguments and returns the error plus
// everything cobra printed. cobra writes both its output and its errors to the
// single writer SetOutput installs, so an empty buffer is the assertion that
// nothing reached stdout.
func execute(args ...string) (error, string) {
	root := hardenedTree()
	var out bytes.Buffer
	root.SetOutput(&out)
	root.SetArgs(args)
	return root.Execute(), out.String()
}

func TestUnknownCommandIsAnError(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		token string
	}{
		{"subcommand of a group", []string{"handbook", "bogus"}, "bogus"},
		{"top-level command", []string{"bogus-top"}, "bogus-top"},
		{"command that only exists at the root", []string{"handbook", "run", "foo"}, "run"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err, out := execute(tc.args...)
			if err == nil {
				t.Fatalf("expected an error, got none\n%s", out)
			}
			if !strings.Contains(err.Error(), "unknown command") || !strings.Contains(err.Error(), tc.token) {
				t.Errorf("error does not name the unrecognized token: %v", err)
			}
			if out != "" {
				t.Errorf("cobra printed to its output instead of leaving the message to main:\n%s", out)
			}
		})
	}
}

// A group command reached from the root with no unrecognized token still gets
// its own help, on cobra's output and without an error.
func TestGroupWithNoArgumentsStillPrintsHelp(t *testing.T) {
	err, out := execute("handbook")
	if err != nil {
		t.Fatalf("expected help, got error: %v", err)
	}
	if !strings.Contains(out, "list") {
		t.Errorf("help does not list the group's subcommands:\n%s", out)
	}
}

func TestUnknownFlagIsAnError(t *testing.T) {
	err, out := execute("handbook", "list", "--clear-body")
	if err == nil {
		t.Fatalf("expected an error, got none\n%s", out)
	}
	if !strings.Contains(err.Error(), "--clear-body") {
		t.Errorf("error does not name the unrecognized flag: %v", err)
	}
	if out != "" {
		t.Errorf("cobra printed help instead of leaving the message to main:\n%s", out)
	}
}

func TestRecognizedCommandStillSucceeds(t *testing.T) {
	if err, out := execute("handbook", "list", "--type", "page"); err != nil {
		t.Fatalf("recognized command failed: %v\n%s", err, out)
	}
	if err, out := execute("run", "build"); err != nil {
		t.Fatalf("recognized root command failed: %v\n%s", err, out)
	}
}

// The suggestion is a convenience on top of the failure, so it is checked
// separately from the exit path it rides on.
func TestNearMissSuggestsTheRealSubcommand(t *testing.T) {
	err, _ := execute("handbook", "lst")
	if err == nil {
		t.Fatal("expected an error, got none")
	}
	if !strings.Contains(err.Error(), "Did you mean this?") || !strings.Contains(err.Error(), "list") {
		t.Errorf("error does not suggest the near-miss subcommand: %v", err)
	}
}
