package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/josh/burrowtime/internal/store"
)

func TestFirstRunOfferImportsWatsonCopy(t *testing.T) {
	watsonDir := t.TempDir()
	burrowDir := t.TempDir()
	t.Setenv("WATSON_DIR", watsonDir)
	t.Setenv("BURROWTIME_DIR", burrowDir)
	seedMigrationFiles(t, watsonDir, "watson-project")

	a := &app{name: "burrowtime"}
	var output bytes.Buffer
	if err := a.offerWatsonMigration(strings.NewReader("\n"), &output, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Watson data was found") || !strings.Contains(output.String(), "Imported 4 Watson data files") {
		t.Fatalf("unexpected prompt/output: %q", output.String())
	}
	assertFileEqual(t, filepath.Join(watsonDir, "frames"), filepath.Join(burrowDir, "frames"))
	if _, err := os.Stat(filepath.Join(burrowDir, "active_timers")); !os.IsNotExist(err) {
		t.Fatalf("import should not invent companion state: %v", err)
	}
	assertFileContains(t, filepath.Join(watsonDir, "state"), "watson-project")
}

func TestFirstRunDeclineIsRemembered(t *testing.T) {
	watsonDir := t.TempDir()
	burrowDir := t.TempDir()
	t.Setenv("WATSON_DIR", watsonDir)
	t.Setenv("BURROWTIME_DIR", burrowDir)
	seedMigrationFiles(t, watsonDir, "watson-project")

	a := &app{name: "burrowtime"}
	var first bytes.Buffer
	if err := a.offerWatsonMigration(strings.NewReader("n\n"), &first, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(burrowDir, migrationOfferedFile)); err != nil {
		t.Fatalf("decline marker: %v", err)
	}
	var second bytes.Buffer
	if err := a.offerWatsonMigration(strings.NewReader("y\n"), &second, true); err != nil {
		t.Fatal(err)
	}
	if second.Len() != 0 {
		t.Fatalf("migration should not be offered twice: %q", second.String())
	}
}

func TestMigrationCommandsRoundTripAndBackup(t *testing.T) {
	watsonDir := t.TempDir()
	burrowDir := t.TempDir()
	seedMigrationFiles(t, watsonDir, "original")

	output, err := runMigrationCommand(burrowDir, "migrate", "from-watson", "--watson-data-dir", watsonDir)
	if err != nil {
		t.Fatalf("import: %v\n%s", err, output)
	}
	assertFileContains(t, filepath.Join(burrowDir, "state"), "original")

	if err := os.WriteFile(filepath.Join(burrowDir, "state"), []byte(`{"project":"burrow","start":20,"tags":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runMigrationCommand(burrowDir, "migrate", "to-watson", "--watson-data-dir", watsonDir); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("noninteractive overwrite should require --force, got %v", err)
	}
	output, err = runMigrationCommand(burrowDir, "migrate", "to-watson", "--watson-data-dir", watsonDir, "--force")
	if err != nil {
		t.Fatalf("export: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Previous destination data was backed up") {
		t.Fatalf("missing backup message: %q", output)
	}
	assertFileContains(t, filepath.Join(watsonDir, "state"), "burrow")
	backups, err := filepath.Glob(filepath.Join(watsonDir, ".burrowtime-backups", "*", "state"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("state backup: matches=%v err=%v", backups, err)
	}
	assertFileContains(t, backups[0], "original")
}

func TestExportRefusesConcurrentTimers(t *testing.T) {
	burrowDir := t.TempDir()
	watsonDir := t.TempDir()
	seedMigrationFiles(t, burrowDir, "primary")
	repository, err := store.New(burrowDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SaveActiveTimers([]store.ActiveTimer{{ID: "abcdef", Project: "second", Start: 10, Tags: []string{}}}); err != nil {
		t.Fatal(err)
	}

	_, err = runMigrationCommand(burrowDir, "migrate", "to-watson", "--watson-data-dir", watsonDir)
	if err == nil || !strings.Contains(err.Error(), "only one running timer") {
		t.Fatalf("expected concurrent export safety error, got %v", err)
	}
}

func runMigrationCommand(dir string, args ...string) (string, error) {
	var output bytes.Buffer
	a := &app{name: "burrowtime"}
	root := a.root()
	root.SetArgs(append([]string{"--data-dir", dir}, args...))
	root.SetIn(strings.NewReader(""))
	root.SetOut(&output)
	root.SetErr(&output)
	err := root.Execute()
	return output.String(), err
}

func seedMigrationFiles(t *testing.T, dir, project string) {
	t.Helper()
	files := map[string]string{
		"config":    "[options]\nstop_on_start = false\n",
		"frames":    `[[1,2,"done","abcdef",[],2]]`,
		"state":     `{"project":"` + project + `","start":10,"tags":[]}`,
		"last_sync": "2",
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func assertFileEqual(t *testing.T, first, second string) {
	t.Helper()
	one, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	two, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(one, two) {
		t.Fatalf("files differ: %s and %s", first, second)
	}
}

func assertFileContains(t *testing.T, path, substring string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), substring) {
		t.Fatalf("%s does not contain %q: %s", path, substring, data)
	}
}
