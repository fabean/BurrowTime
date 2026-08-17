package watson

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fabean/BurrowTime/internal/store"
)

func TestEditPreservesIDAndMergePreservesIncomingMetadata(t *testing.T) {
	dir := t.TempDir()
	repo, _ := store.New(dir)
	stop1, stop2 := int64(20), int64(40)
	frames := []store.Frame{{Start: 10, Stop: &stop1, Project: "one", ID: "11111111111111111111111111111111", Tags: []string{}, UpdatedAt: 20}, {Start: 30, Stop: &stop2, Project: "two", ID: "22222222222222222222222222222222", Tags: []string{}, UpdatedAt: 40}}
	if err := repo.SaveFrames(frames); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return time.Unix(100, 0).UTC() }
	edited, err := s.EditFrame("1111111", "changed", []string{"x"}, time.Unix(11, 0), time.Unix(21, 0))
	if err != nil {
		t.Fatal(err)
	}
	if edited.ID != frames[0].ID || edited.UpdatedAt != 100 {
		t.Fatalf("edited=%#v", edited)
	}
	incoming := `[[30,45,"conflict","22222222222222222222222222222222",[],45],[50,60,"new","33333333333333333333333333333333",["z"],60]]`
	path := filepath.Join(dir, "incoming")
	if err := os.WriteFile(path, []byte(incoming), 0o600); err != nil {
		t.Fatal(err)
	}
	conflicts, merging, err := s.MergeReport(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 1 || len(merging) != 1 {
		t.Fatalf("conflicts=%d merging=%d", len(conflicts), len(merging))
	}
	if err := s.ApplyMerge(map[string]store.Frame{conflicts[0].ID: conflicts[0]}, merging); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.LoadFrames()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 3 || loaded[1].Project != "conflict" || loaded[1].UpdatedAt != 45 || loaded[2].UpdatedAt != 60 {
		t.Fatalf("merged=%#v", loaded)
	}
}
