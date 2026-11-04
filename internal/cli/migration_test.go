package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fabean/BurrowTime/internal/store"
	"github.com/spf13/cobra"
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

func TestMigrationRejectsSameDirectoryThroughSymlink(t *testing.T) {
	source := t.TempDir()
	seedMigrationFiles(t, source, "original")
	if err := os.WriteFile(filepath.Join(source, "active_timers"), []byte("companion history"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(source, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if same, err := samePath(source, alias); err != nil || !same {
		t.Fatalf("same directory alias not detected: same=%v err=%v", same, err)
	}
	if _, err := migrateCompatibleData(source, alias, true, "from-watson"); err == nil || !strings.Contains(err.Error(), "same") {
		t.Fatalf("aliased migration accepted: %v", err)
	}
	assertFileContains(t, filepath.Join(source, "state"), "original")
	assertFileContains(t, filepath.Join(source, "active_timers"), "companion history")
	if fileExists(filepath.Join(source, ".burrowtime-backups")) {
		t.Fatal("rejected migration created a backup inside the source")
	}
}

func TestMigrationRejectsInvalidSourceAndDestinationWithoutReplacingData(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*testing.T, string, string)
		want  string
	}{
		{name: "missing source", want: "no compatible", setup: func(t *testing.T, source, destination string) {
			if err := os.RemoveAll(source); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "directory source", want: "not a regular file", setup: func(t *testing.T, source, destination string) {
			if err := os.Mkdir(filepath.Join(source, "frames"), 0700); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "destination is file", want: "not a directory", setup: func(t *testing.T, source, destination string) {
			seedMigrationFiles(t, source, "source")
			if err := os.RemoveAll(destination); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(destination, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "directory destination entry", want: "not a regular file", setup: func(t *testing.T, source, destination string) {
			seedMigrationFiles(t, source, "source")
			if err := os.Mkdir(filepath.Join(destination, "frames"), 0700); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "backup path blocked", want: "not a directory", setup: func(t *testing.T, source, destination string) {
			seedMigrationFiles(t, source, "source")
			if err := os.WriteFile(filepath.Join(destination, "frames"), []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(destination, ".burrowtime-backups"), []byte("blocked"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source, destination := t.TempDir(), t.TempDir()
			tc.setup(t, source, destination)
			_, err := migrateCompatibleData(source, destination, true, "from-watson")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
			if fileExists(filepath.Join(destination, "state")) {
				t.Fatal("failed validation copied source data")
			}
		})
	}
}

func TestMigrationSparseImportBacksUpAndClearsOldCompanionData(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "frames"), []byte("[]"), 0640); err != nil {
		t.Fatal(err)
	}
	seedMigrationFiles(t, destination, "previous")
	if err := os.WriteFile(filepath.Join(destination, "active_timers"), []byte("old timers"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := markMigrationOffered(destination); err != nil {
		t.Fatal(err)
	}
	result, err := migrateCompatibleData(source, destination, true, "from-watson")
	if err != nil {
		t.Fatal(err)
	}
	if result.Copied != 1 || result.Backup == "" {
		t.Fatalf("result=%#v", result)
	}
	assertFileContains(t, filepath.Join(destination, "frames"), "[]")
	assertFileContains(t, filepath.Join(result.Backup, "active_timers"), "old timers")
	assertFileContains(t, filepath.Join(result.Backup, "state"), "previous")
	for _, name := range []string{"state", "config", "last_sync", "active_timers", migrationOfferedFile} {
		if fileExists(filepath.Join(destination, name)) {
			t.Fatalf("stale target %s survived sparse import", name)
		}
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(destination, "frames"))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0640 {
			t.Fatalf("migrated mode=%v", info.Mode().Perm())
		}
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".migration-") {
			t.Fatalf("temporary file remains: %s", entry.Name())
		}
	}
}

func TestMigrationCommandsDefaultWatsonDirectoryAndSourceErrors(t *testing.T) {
	watsonDir, burrowDir := t.TempDir(), t.TempDir()
	t.Setenv("WATSON_DIR", watsonDir)
	seedMigrationFiles(t, watsonDir, "source")
	if output, err := runMigrationCommand(burrowDir, "migrate", "from-watson"); err != nil || !strings.Contains(output, "Imported 4") {
		t.Fatalf("default import: output=%q err=%v", output, err)
	}
	if output, err := runMigrationCommand(burrowDir, "migrate", "to-watson", "--force"); err != nil || !strings.Contains(output, "Exported 4") {
		t.Fatalf("default export: output=%q err=%v", output, err)
	}
	if _, err := runMigrationCommand(t.TempDir(), "migrate", "to-watson"); err == nil || !strings.Contains(err.Error(), "No BurrowTime data") {
		t.Fatalf("empty export err=%v", err)
	}
	if _, err := runMigrationCommand(t.TempDir(), "migrate", "from-watson", "--watson-data-dir", t.TempDir()); err == nil || !strings.Contains(err.Error(), "No Watson data") {
		t.Fatalf("empty import err=%v", err)
	}
	if err := os.WriteFile(filepath.Join(burrowDir, "active_timers"), []byte("broken JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runMigrationCommand(burrowDir, "migrate", "to-watson"); err == nil || !strings.Contains(err.Error(), "Invalid JSON") {
		t.Fatalf("corrupt companion err=%v", err)
	}
}

func TestMigrationOfferSkipsAndPropagatesFailures(t *testing.T) {
	cases := []struct {
		name        string
		appName     string
		interactive bool
		same        bool
		sourceData  bool
		targetData  bool
	}{
		{name: "noninteractive", appName: "burrowtime", sourceData: true},
		{name: "Watson alias", appName: "watson", interactive: true, sourceData: true},
		{name: "same directory", appName: "burrowtime", interactive: true, sourceData: true, same: true},
		{name: "no source", appName: "burrowtime", interactive: true},
		{name: "existing destination", appName: "burrowtime", interactive: true, sourceData: true, targetData: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source, destination := t.TempDir(), t.TempDir()
			if tc.same {
				destination = source
			}
			t.Setenv("WATSON_DIR", source)
			if tc.sourceData {
				seedMigrationFiles(t, source, "source")
			}
			if tc.targetData {
				seedMigrationFiles(t, destination, "destination")
			}
			var out bytes.Buffer
			a := &app{name: tc.appName, dir: destination}
			if err := a.offerWatsonMigration(strings.NewReader("y\n"), &out, tc.interactive); err != nil || out.Len() != 0 {
				t.Fatalf("skip offer output=%q err=%v", out.String(), err)
			}
		})
	}
	t.Run("read error", func(t *testing.T) {
		source := t.TempDir()
		seedMigrationFiles(t, source, "source")
		t.Setenv("WATSON_DIR", source)
		want := errors.New("input failed")
		a := &app{name: "burrowtime", dir: t.TempDir()}
		if err := a.offerWatsonMigration(migrationErrorReader{err: want}, io.Discard, true); !errors.Is(err, want) {
			t.Fatalf("reader error=%v", err)
		}
	})
	t.Run("cannot save decline marker", func(t *testing.T) {
		source := t.TempDir()
		seedMigrationFiles(t, source, "source")
		t.Setenv("WATSON_DIR", source)
		destination := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(destination, []byte("blocked"), 0600); err != nil {
			t.Fatal(err)
		}
		a := &app{name: "burrowtime", dir: destination}
		if err := a.offerWatsonMigration(strings.NewReader("no\n"), io.Discard, true); err == nil {
			t.Fatal("failed marker write not reported")
		}
	})
	t.Run("invalid source file", func(t *testing.T) {
		source := t.TempDir()
		seedMigrationFiles(t, source, "source")
		t.Setenv("WATSON_DIR", source)
		if err := os.Remove(filepath.Join(source, "config")); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(source, "config"), 0700); err != nil {
			t.Fatal(err)
		}
		a := &app{name: "burrowtime", dir: t.TempDir()}
		if err := a.offerWatsonMigration(strings.NewReader("yes\n"), io.Discard, true); err == nil {
			t.Fatal("failed import not reported")
		}
	})
}

func TestMigrationConfirmationAnswers(t *testing.T) {
	cases := []struct {
		name, input      string
		defaultYes, want bool
		reprompt         bool
	}{
		{name: "default yes", input: "\n", defaultYes: true, want: true},
		{name: "default no", input: "\n"},
		{name: "yes case insensitive", input: " YES \n", want: true},
		{name: "no case insensitive", input: " NO \n"},
		{name: "retry", input: "maybe\ny\n", want: true, reprompt: true},
		{name: "invalid EOF", input: "maybe", defaultYes: true, want: true, reprompt: true},
		{name: "empty EOF"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			got, err := askYesNo(strings.NewReader(tc.input), &output, "Continue? ", tc.defaultYes)
			if err != nil || got != tc.want || strings.Contains(output.String(), "Please answer") != tc.reprompt {
				t.Fatalf("answer=%v output=%q err=%v", got, output.String(), err)
			}
		})
	}
	want := errors.New("input failed")
	if _, err := askYesNo(migrationErrorReader{err: want}, io.Discard, "Continue? ", true); !errors.Is(err, want) {
		t.Fatalf("read error=%v", err)
	}
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader("yes\n"))
	cmd.SetOut(io.Discard)
	dir := t.TempDir()
	if err := confirmMigrationOverwrite(cmd, dir, false, "Watson"); err != nil {
		t.Fatal(err)
	}
	seedMigrationFiles(t, dir, "existing")
	if err := confirmMigrationOverwrite(cmd, dir, true, "Watson"); err != nil {
		t.Fatal(err)
	}
	if err := confirmMigrationOverwrite(cmd, dir, false, "Watson"); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("unattended overwrite accepted: %v", err)
	}
}

type migrationErrorReader struct{ err error }

func (r migrationErrorReader) Read([]byte) (int, error) { return 0, r.err }

func TestMigrationPropagatesDefaultDirectoryResolutionErrors(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("relative XDG config paths are rejected on Linux")
	}
	t.Setenv("XDG_CONFIG_HOME", "relative")
	t.Setenv("BURROWTIME_DIR", "")
	t.Setenv("WATSON_DIR", "")
	for _, direction := range []string{"from-watson", "to-watson"} {
		t.Run(direction+" destination", func(t *testing.T) {
			a := &app{name: "burrowtime"}
			var cmd *cobra.Command
			if direction == "from-watson" {
				cmd = a.migrateFromWatson()
			} else {
				cmd = a.migrateToWatson()
			}
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); err == nil {
				t.Fatal("unresolvable directory error discarded")
			}
		})
		t.Run(direction+" Watson default", func(t *testing.T) {
			dir := t.TempDir()
			seedMigrationFiles(t, dir, "source")
			a := &app{name: "burrowtime", dir: dir}
			var cmd *cobra.Command
			if direction == "from-watson" {
				cmd = a.migrateFromWatson()
			} else {
				cmd = a.migrateToWatson()
			}
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); err == nil {
				t.Fatal("unresolvable Watson directory error discarded")
			}
		})
	}
	a := &app{name: "burrowtime"}
	if err := a.offerWatsonMigration(strings.NewReader("yes\n"), io.Discard, true); err == nil {
		t.Fatal("offer ignored destination resolution error")
	}
	a.dir = t.TempDir()
	if err := a.offerWatsonMigration(strings.NewReader("yes\n"), io.Discard, true); err == nil {
		t.Fatal("offer ignored source resolution error")
	}
}

func TestMigrationCommandSafeguardsDoNotReplaceData(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	seedMigrationFiles(t, source, "source")
	seedMigrationFiles(t, destination, "destination")
	if _, err := runMigrationCommand(destination, "migrate", "from-watson", "--watson-data-dir", source); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("existing import destination accepted: %v", err)
	}
	assertFileContains(t, filepath.Join(destination, "state"), "destination")
	if _, err := runMigrationCommand(source, "migrate", "from-watson", "--watson-data-dir", source, "--force"); err == nil || !strings.Contains(err.Error(), "same") {
		t.Fatalf("same import directory accepted: %v", err)
	}
	if _, err := runMigrationCommand(source, "migrate", "to-watson", "--watson-data-dir", source, "--force"); err == nil || !strings.Contains(err.Error(), "same") {
		t.Fatalf("same export directory accepted: %v", err)
	}
	assertFileContains(t, filepath.Join(source, "state"), "source")
}

func TestMigrationPermissionFailuresPreserveDestination(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission failures require Unix file permissions and a non-root user")
	}
	cases := []struct {
		name string
		deny func(*testing.T, string, string)
		want string
	}{
		{name: "unreadable source", want: "permission denied", deny: func(t *testing.T, source, destination string) {
			migrationDenyPermissions(t, filepath.Join(source, "frames"), 0)
		}},
		{name: "unsearchable source", want: "permission denied", deny: func(t *testing.T, source, destination string) { migrationDenyPermissions(t, source, 0) }},
		{name: "unreadable destination", want: "permission denied", deny: func(t *testing.T, source, destination string) {
			migrationDenyPermissions(t, filepath.Join(destination, "frames"), 0)
		}},
		{name: "unsearchable destination", want: "permission denied", deny: func(t *testing.T, source, destination string) { migrationDenyPermissions(t, destination, 0) }},
		{name: "unwritable backup", want: "permission denied", deny: func(t *testing.T, source, destination string) {
			backup := filepath.Join(destination, ".burrowtime-backups")
			if err := os.Mkdir(backup, 0700); err != nil {
				t.Fatal(err)
			}
			migrationDenyPermissions(t, backup, 0500)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source, destination := t.TempDir(), t.TempDir()
			seedMigrationFiles(t, source, "source")
			seedMigrationFiles(t, destination, "destination")
			tc.deny(t, source, destination)
			if _, err := migrateCompatibleData(source, destination, true, "from-watson"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestAtomicMigrationWriteFailuresCleanTemporaryFiles(t *testing.T) {
	cases := []struct {
		name string
		path func(*testing.T, string) string
	}{
		{name: "missing directory", path: func(t *testing.T, dir string) string { return filepath.Join(dir, "missing", "frames") }},
		{name: "overlong filename", path: func(t *testing.T, dir string) string { return filepath.Join(dir, strings.Repeat("f", 300)) }},
		{name: "symlink loop", path: func(t *testing.T, dir string) string {
			path := filepath.Join(dir, "loop")
			if err := os.Symlink("loop", path); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			return path
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := tc.path(t, dir)
			if err := atomicWriteMigrationFile(path, []byte("new data"), 0600); err == nil {
				t.Fatal("invalid migration target accepted")
			}
			matches, err := filepath.Glob(filepath.Join(dir, ".migration-*"))
			if err != nil {
				t.Fatal(err)
			}
			if len(matches) != 0 {
				t.Fatalf("failed migration leaked temporary files: %v", matches)
			}
		})
	}
}

func migrationDenyPermissions(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(path, info.Mode().Perm()); err != nil {
			t.Errorf("restore permissions: %v", err)
		}
	})
}
