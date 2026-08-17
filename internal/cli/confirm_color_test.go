package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewProjectConfirmationCanAbortWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	var output bytes.Buffer
	a := &app{}
	root := a.root()
	root.SetArgs([]string{"--watson-dir", dir, "start", "--confirm-new-project", "new project"})
	root.SetIn(strings.NewReader("n\n"))
	root.SetOut(&output)
	root.SetErr(&output)
	if err := root.Execute(); err == nil || err.Error() != "Aborted!" {
		t.Fatalf("error=%v output=%q", err, output.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
		t.Fatalf("aborted start wrote state: %v", err)
	}
}

func TestForcedColorMatchesClickStyle(t *testing.T) {
	dir := t.TempDir()
	data := `[[1,2,"project","aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",["tag"],2]]`
	os.WriteFile(filepath.Join(dir, "frames"), []byte(data), 0o600)
	var output bytes.Buffer
	a := &app{}
	root := a.root()
	root.SetArgs([]string{"--watson-dir", dir, "--color", "projects"})
	root.SetOut(&output)
	root.SetErr(&output)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "\x1b[35mproject\x1b[0m\n"; got != want {
		t.Fatalf("want %q got %q", want, got)
	}
}
