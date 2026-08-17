package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/josh/burrowtime/internal/store"
)

func TestEditCommandRunsEditorAndUpdatesFrame(t *testing.T) {
	dir := t.TempDir()
	repo, _ := store.New(dir)
	stop := int64(1_550_000_000)
	frame := store.Frame{Start: 1_549_999_000, Stop: &stop, Project: "old", ID: "11111111111111111111111111111111", Tags: []string{}, UpdatedAt: stop}
	if err := repo.SaveFrames([]store.Frame{frame}); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "editor")
	body := "#!/bin/sh\nprintf '%s' '{\"project\":\"new\",\"start\":\"2019-02-12 09:00:00\",\"stop\":\"2019-02-12 10:00:00\",\"tags\":[\"edited\"]}' > \"$1\"\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", script)
	var output bytes.Buffer
	a := &app{}
	root := a.root()
	root.SetArgs([]string{"--watson-dir", dir, "edit", "--", "-1"})
	root.SetOut(&output)
	root.SetErr(&output)
	if err := root.Execute(); err != nil {
		t.Fatalf("edit: %v\n%s", err, output.String())
	}
	loaded, err := repo.LoadFrames()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].ID != frame.ID || loaded[0].Project != "new" || len(loaded[0].Tags) != 1 || loaded[0].Tags[0] != "edited" {
		t.Fatalf("edited frame=%#v", loaded)
	}
	if !strings.Contains(output.String(), "Edited frame for project new [edited]") {
		t.Fatalf("output=%q", output.String())
	}
}

func TestMergeCommandResolvesRightAndAddsNewFrame(t *testing.T) {
	dir := t.TempDir()
	repo, _ := store.New(dir)
	stop := int64(20)
	if err := repo.SaveFrames([]store.Frame{{Start: 10, Stop: &stop, Project: "left", ID: "11111111111111111111111111111111", Tags: []string{}, UpdatedAt: 20}}); err != nil {
		t.Fatal(err)
	}
	incoming := filepath.Join(t.TempDir(), "frames")
	data := `[[10,25,"right","11111111111111111111111111111111",[],25],[30,40,"new","22222222222222222222222222222222",[],40]]`
	if err := os.WriteFile(incoming, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	a := &app{}
	root := a.root()
	root.SetArgs([]string{"--watson-dir", dir, "merge", incoming, "--force"})
	root.SetIn(strings.NewReader("r\n"))
	root.SetOut(&output)
	root.SetErr(&output)
	if err := root.Execute(); err != nil {
		t.Fatalf("merge: %v\n%s", err, output.String())
	}
	loaded, err := repo.LoadFrames()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 || loaded[0].Project != "right" || loaded[1].Project != "new" {
		t.Fatalf("merged=%#v", loaded)
	}
	if !strings.Contains(output.String(), "1 frames will need to be resolved") || !strings.Contains(output.String(), "**right**") {
		t.Fatalf("output=%s", output.String())
	}
}
