package cli

import (
	"bytes"
	"reflect"
	"testing"
)

func TestClickUsageFailureFormatting(t *testing.T) {
	a := &app{name: "watson"}
	root := a.root()
	root.SetArgs([]string{"frobnicate"})
	command, err := root.ExecuteC()
	if err == nil || !isUsageFailure(err) {
		t.Fatalf("expected usage failure, got %v", err)
	}
	want := "Usage: watson [OPTIONS] COMMAND [ARGS]...\n" +
		"Try 'watson --help' for help.\n\n" +
		"Error: No such command 'frobnicate'.\n\n" +
		"Did you mean one of these?\n    frames"
	if got := formatUsageFailure(command, err); got != want {
		t.Fatalf("usage text differs\nwant: %q\n got: %q", want, got)
	}
	typed := &exitError{code: 2, message: want}
	if ExitCode(typed) != 2 || FormatError(typed) != want {
		t.Fatal("typed usage error did not retain Click status and text")
	}
}

func TestNormalizeIgnoredOptions(t *testing.T) {
	root := (&app{name: "watson"}).root()
	input := []string{"add", "--bad", "project", "--from", "10:00", "--to", "11:00"}
	want := []string{"add", "--from", "10:00", "--to", "11:00", "--", "--bad", "project"}
	if got := normalizeIgnoredOptions(root, input); !reflect.DeepEqual(got, want) {
		t.Fatalf("want %#v, got %#v", want, got)
	}
}

func TestPairedOutputFormatUsesLastFlag(t *testing.T) {
	dir := t.TempDir()
	a := &app{name: "watson"}
	root := a.root()
	var output bytes.Buffer
	root.SetArgs([]string{"--watson-dir", dir, "report", "--json", "--csv"})
	root.SetOut(&output)
	root.SetErr(&output)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if output.String() != "\n" {
		t.Fatalf("last --csv should select empty CSV output, got %q", output.String())
	}
}
