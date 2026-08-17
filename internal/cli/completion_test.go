package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDynamicCompletionsUseWatsonFrames(t *testing.T) {
	dir := t.TempDir()
	data := `[[1,2,"project-one","aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",["tag-one"],2],[3,4,"project-two","bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",["tag-two"],4]]`
	if err := os.WriteFile(filepath.Join(dir, "frames"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &app{dir: dir}
	projects, _ := a.completeProjects(nil, nil, "project-t")
	if !reflect.DeepEqual(projects, []string{"project-two"}) {
		t.Fatalf("projects=%#v", projects)
	}
	tags, _ := a.completeProjectOrTag(nil, []string{"project-one"}, "+tag-")
	if !reflect.DeepEqual(tags, []string{"+tag-one", "+tag-two"}) {
		t.Fatalf("tags=%#v", tags)
	}
	frames, _ := a.completeFrames(nil, nil, "b")
	if !reflect.DeepEqual(frames, []string{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}) {
		t.Fatalf("frames=%#v", frames)
	}
	if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
		t.Fatalf("completion mutated data: %v", err)
	}
}
