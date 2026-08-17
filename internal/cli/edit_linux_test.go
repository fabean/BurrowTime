//go:build linux

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fabean/BurrowTime/internal/store"
)

func TestEditCommandPassesFileStdinDirectlyToEditor(t *testing.T) {
	dir := t.TempDir()
	repo, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveState(store.State{Project: "old", Start: 1_549_999_000, Tags: []string{"before"}}); err != nil {
		t.Fatal(err)
	}

	stdinPath := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(stdinPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stdin, err := os.Open(stdinPath)
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()

	script := filepath.Join(t.TempDir(), "editor")
	body := `#!/bin/sh
actual_stdin="$(readlink /proc/self/fd/0)"
if [ "$actual_stdin" != "$EXPECTED_STDIN" ]; then
  printf 'editor stdin was %s, want %s\n' "$actual_stdin" "$EXPECTED_STDIN" >&2
  exit 42
fi
printf '%s' '{"project":"new","start":"2019-02-12 09:00:00","tags":["edited"]}' > "$1"
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", script)
	t.Setenv("EXPECTED_STDIN", stdinPath)

	var output bytes.Buffer
	a := &app{}
	root := a.root()
	root.SetArgs([]string{"--watson-dir", dir, "edit"})
	root.SetIn(stdin)
	root.SetOut(&output)
	root.SetErr(&output)
	if err := root.Execute(); err != nil {
		t.Fatalf("edit: %v\n%s", err, output.String())
	}

	state, err := repo.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Project != "new" || len(state.Tags) != 1 || state.Tags[0] != "edited" {
		t.Fatalf("edited state=%#v", state)
	}
	if !strings.Contains(output.String(), "Edited frame for project new [edited]") {
		t.Fatalf("output=%q", output.String())
	}
}
