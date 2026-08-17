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
