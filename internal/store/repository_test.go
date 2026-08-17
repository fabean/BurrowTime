package store

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReadsLegacyWatsonFrames(t *testing.T) {
	dir := t.TempDir()
	data := `[[4000, 4010, "foo", "abcdef012345", ["A", "B"]], [4020, null, "running", null]]`
	if err := os.WriteFile(filepath.Join(dir, "frames"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	repo, _ := New(dir)
	frames, err := repo.LoadFrames()
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 {
		t.Fatalf("got %d frames", len(frames))
	}
	if frames[0].Project != "foo" || !reflect.DeepEqual(frames[0].Tags, []string{"A", "B"}) {
		t.Fatalf("unexpected frame: %#v", frames[0])
	}
	if frames[1].Stop != nil || len(frames[1].Tags) != 0 {
		t.Fatalf("legacy defaults not applied: %#v", frames[1])
	}
	if !frames[1].IDNull {
		t.Fatal("null frame id was not preserved")
	}
}

func TestWatsonArrayEncodingAndBackup(t *testing.T) {
	dir := t.TempDir()
	repo, _ := New(dir)
	stop := int64(20)
	first := []Frame{{Start: 10, Stop: &stop, Project: "café", ID: "abc", Tags: []string{"one"}, UpdatedAt: 30}}
	if err := repo.SaveFrames(first); err != nil {
		t.Fatal(err)
	}
	want := "[\n [\n  10,\n  20,\n  \"café\",\n  \"abc\",\n  [\n   \"one\"\n  ],\n  30\n ]\n]"
	got, err := os.ReadFile(filepath.Join(dir, "frames"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("Watson encoding differs\nwant: %s\n got: %s", want, got)
	}
	if err := repo.SaveFrames([]Frame{}); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(filepath.Join(dir, "frames.bak"))
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != want {
		t.Fatal("backup does not contain previous data")
	}
}

func TestWatsonJSONDoesNotHTMLEscape(t *testing.T) {
	dir := t.TempDir()
	repo, _ := New(dir)
	stop := int64(20)
	frame := Frame{Start: 10, Stop: &stop, Project: "café <&>", ID: "abc", Tags: []string{"😀"}, UpdatedAt: 30}
	if err := repo.SaveFrames([]Frame{frame}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "frames"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"café <&>", "😀"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Watson-compatible JSON should contain %q literally: %s", want, text)
		}
	}
}

func TestReadsDoNotCreateOrRewriteFiles(t *testing.T) {
	dir := t.TempDir()
	repo, _ := New(dir)
	if _, err := repo.LoadFrames(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("read created files: %v", entries)
	}
}

func TestActiveTimersUseACompanionWithoutChangingWatsonState(t *testing.T) {
	dir := t.TempDir()
	repo, _ := New(dir)
	state := State{Project: "primary", Start: 100, Tags: []string{"watson"}}
	if err := repo.SaveState(state); err != nil {
		t.Fatal(err)
	}
	stateBefore, err := os.ReadFile(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	timers := []ActiveTimer{{ID: "abcdef", Project: "second", Start: 200, Tags: []string{"parallel"}}}
	if err := repo.SaveActiveTimers(timers); err != nil {
		t.Fatal(err)
	}
	stateAfter, err := os.ReadFile(filepath.Join(dir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	if string(stateAfter) != string(stateBefore) {
		t.Fatalf("companion state changed Watson state\nbefore: %s\n after: %s", stateBefore, stateAfter)
	}
	loaded, err := repo.LoadActiveTimers()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, timers) {
		t.Fatalf("loaded %#v, want %#v", loaded, timers)
	}
}
