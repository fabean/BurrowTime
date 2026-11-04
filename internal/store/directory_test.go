package store

import (
	"path/filepath"
	"testing"
)

func TestApplicationDirectoriesUseSeparateEnvironmentOverrides(t *testing.T) {
	watson := filepath.Join(t.TempDir(), "watson")
	burrow := filepath.Join(t.TempDir(), "burrowtime")
	t.Setenv("WATSON_DIR", watson)
	t.Setenv("BURROWTIME_DIR", burrow)

	gotWatson, err := WatsonDir()
	if err != nil {
		t.Fatal(err)
	}
	gotBurrow, err := BurrowTimeDir()
	if err != nil {
		t.Fatal(err)
	}
	if gotWatson != watson || gotBurrow != burrow || gotWatson == gotBurrow {
		t.Fatalf("watson=%q burrowtime=%q", gotWatson, gotBurrow)
	}
}

func TestDefaultApplicationDirectories(t *testing.T) {
	t.Setenv("WATSON_DIR", "")
	t.Setenv("BURROWTIME_DIR", "")
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	for _, tt := range []struct {
		name    string
		resolve func() (string, error)
	}{{"watson", WatsonDir}, {"burrowtime", BurrowTimeDir}} {
		t.Run(tt.name, func(t *testing.T) {
			path, err := tt.resolve()
			if err != nil || filepath.Base(path) != tt.name {
				t.Fatalf("path=%q err=%v", path, err)
			}
		})
	}
	repo, err := New("")
	if err != nil || filepath.Base(repo.Dir) != "burrowtime" {
		t.Fatalf("repo=%#v err=%v", repo, err)
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if _, err := New(""); err == nil {
		t.Fatal("missing home should fail")
	}
}
