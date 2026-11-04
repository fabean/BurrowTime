package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
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

func TestCompletionContextsAndReadFailures(t *testing.T) {
	dir := t.TempDir()
	if _, err := runBurrowTimeCommand(
		dir, "add", "project", "+tag", "--from", "2020-01-01 09:00", "--to", "2020-01-01 10:00",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := runBurrowTimeCommand(dir, "start", "project", "+tag"); err != nil {
		t.Fatal(err)
	}
	a := &app{dir: dir}
	for _, tc := range []struct {
		name     string
		complete func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)
		args     []string
		prefix   string
		want     []string
	}{
		{name: "project", complete: a.completeProjectOrTag, prefix: "pro", want: []string{"project"}},
		{name: "tag after tag", complete: a.completeProjectOrTag, args: []string{"project", "+existing"}, prefix: "ta", want: []string{"+tag"}},
		{name: "timer", complete: a.completeTimers, prefix: "pri", want: []string{"primary"}},
		{name: "rename kinds", complete: a.completeRename, prefix: "p", want: []string{"project"}},
		{name: "rename project", complete: a.completeRename, args: []string{"project"}, want: []string{"project"}},
		{name: "rename tag", complete: a.completeRename, args: []string{"tag"}, want: []string{"tag"}},
		{name: "rename new value", complete: a.completeRename, args: []string{"tag", "old"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, directive := tc.complete(nil, tc.args, tc.prefix)
			if !reflect.DeepEqual(got, tc.want) || directive != cobra.ShellCompDirectiveNoFileComp {
				t.Fatalf("got=%v directive=%v", got, directive)
			}
		})
	}
	if err := os.WriteFile(filepath.Join(dir, "frames"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		complete func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)
	}{
		{name: "projects", complete: a.completeProjects},
		{name: "tags", complete: a.completeTags},
		{name: "frames", complete: a.completeFrames},
		{name: "timers", complete: a.completeTimers},
	} {
		t.Run(tc.name+" read failure", func(t *testing.T) {
			if values, directive := tc.complete(nil, nil, ""); directive != cobra.ShellCompDirectiveError || len(values) != 0 {
				t.Fatalf("values=%v directive=%v", values, directive)
			}
		})
	}
}
