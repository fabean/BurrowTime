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

func TestAgentSessionsUseASeparateVersionedCompanion(t *testing.T) {
	dir := t.TempDir()
	repo, _ := New(dir)
	sessions := []AgentSession{{
		ID: "session", TimerID: "primary", Client: "codex", Project: "burrowtime", Task: "BT-42",
		Tags: []string{"BT-42"}, Status: AgentSessionActive, StartedAt: 10, HeartbeatAt: 20,
		LeaseSeconds: 900, FrameIDs: []string{},
	}}
	if err := repo.SaveAgentSessions(sessions); err != nil {
		t.Fatal(err)
	}
	loaded, err := repo.LoadAgentSessions()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, sessions) {
		t.Fatalf("loaded %#v, want %#v", loaded, sessions)
	}
	for _, watsonFile := range []string{"frames", "state"} {
		if _, err := os.Stat(filepath.Join(dir, watsonFile)); !os.IsNotExist(err) {
			t.Fatalf("agent session write created %s", watsonFile)
		}
	}
}

func TestRepositoryCompanionDefaultsAndValidation(t *testing.T) {
	tests := []struct {
		name, file, invalid string
		load                func(*Repository) error
	}{
		{"frames", "frames", `{}`, func(r *Repository) error { _, err := r.LoadFrames(); return err }},
		{"state", "state", `[]`, func(r *Repository) error { _, err := r.LoadState(); return err }},
		{"active", "active_timers", `{"version":2}`, func(r *Repository) error { _, err := r.LoadActiveTimers(); return err }},
		{"agents", "agent_sessions", `{"version":2}`, func(r *Repository) error { _, err := r.LoadAgentSessions(); return err }},
		{"messages", "session_messages", `{"version":2}`, func(r *Repository) error { _, err := r.LoadSessionMessages(); return err }},
		{"last sync", "last_sync", `"bad"`, func(r *Repository) error { _, err := r.LoadLastSync(); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := New(t.TempDir())
			for _, data := range []string{"", "  \n", tt.invalid, "{"} {
				if err := os.WriteFile(r.path(tt.file), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
				err := tt.load(r)
				if strings.TrimSpace(data) == "" {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil {
					t.Fatalf("accepted %q", data)
				}
			}
			if err := os.Remove(r.path(tt.file)); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(r.path(tt.file), 0700); err != nil {
				t.Fatal(err)
			}
			if err := tt.load(r); err == nil {
				t.Fatal("read directory should fail")
			}
		})
	}
	r, _ := New(t.TempDir())
	if err := r.SaveState(State{Project: "running"}); err != nil {
		t.Fatal(err)
	}
	state, err := r.LoadState()
	if err != nil || state.Tags == nil {
		t.Fatalf("state=%#v err=%v", state, err)
	}
	if err := r.SaveState(State{}); err != nil {
		t.Fatal(err)
	}
	state, err = r.LoadState()
	if err != nil || state.Running() {
		t.Fatalf("state=%#v err=%v", state, err)
	}
	if err := r.SaveActiveTimers([]ActiveTimer{{ID: "timer", Project: "p", Primary: true}}); err != nil {
		t.Fatal(err)
	}
	timers, err := r.LoadActiveTimers()
	if err != nil || len(timers) != 1 || timers[0].Primary || timers[0].Tags == nil {
		t.Fatalf("timers=%#v err=%v", timers, err)
	}
	if err := r.SaveAgentSessions([]AgentSession{{ID: "session"}}); err != nil {
		t.Fatal(err)
	}
	sessions, err := r.LoadAgentSessions()
	if err != nil || sessions[0].Tags == nil || sessions[0].FrameIDs == nil {
		t.Fatalf("sessions=%#v err=%v", sessions, err)
	}
	if err := r.SaveSessionMessages(SessionMessages{}); err != nil {
		t.Fatal(err)
	}
	messages, err := r.LoadSessionMessages()
	if err != nil || messages.Version != 1 || messages.Frames == nil || messages.Timers == nil {
		t.Fatalf("messages=%#v err=%v", messages, err)
	}
	if err := os.WriteFile(r.path("session_messages"), []byte(`{"version":1,"frames":null,"timers":null}`), 0600); err != nil {
		t.Fatal(err)
	}
	messages, err = r.LoadSessionMessages()
	if err != nil || messages.Frames == nil || messages.Timers == nil {
		t.Fatalf("messages=%#v err=%v", messages, err)
	}
	if err := r.SaveLastSync(123456); err != nil {
		t.Fatal(err)
	}
	value, err := r.LoadLastSync()
	if err != nil || value != 123456 {
		t.Fatalf("last sync=%d err=%v", value, err)
	}
	if err := r.SaveConfig([]byte("[options]\n")); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryWriteFailurePreservesExistingFile(t *testing.T) {
	dir := t.TempDir()
	r, _ := New(dir)
	if err := r.SaveFrames([]Frame{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(r.path("frames.bak"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.path("frames.bak"), "keep"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveFrames([]Frame{{Project: "replacement"}}); err == nil {
		t.Fatal("replacement should fail with nonempty backup directory")
	}
	data, err := os.ReadFile(r.path("frames"))
	if err != nil || string(data) != "[]" {
		t.Fatalf("original=%s err=%v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".frames-") {
			t.Fatalf("leaked temporary file %s", entry.Name())
		}
	}
	if err := r.saveJSON("unsupported", make(chan int)); err == nil {
		t.Fatal("unsupported JSON should fail")
	}
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	bad, _ := New(path)
	if err := bad.SaveFrames(nil); err == nil {
		t.Fatal("file cannot be repository directory")
	}
}

func TestLegacyCompanionListsNormalizeNulls(t *testing.T) {
	r, _ := New(t.TempDir())
	if err := os.WriteFile(r.path("active_timers"), []byte(`{"version":0,"timers":[{"id":"timer","tags":null}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	timers, err := r.LoadActiveTimers()
	if err != nil || timers[0].Tags == nil {
		t.Fatalf("timers=%#v err=%v", timers, err)
	}
	if err := os.WriteFile(r.path("agent_sessions"), []byte(`{"version":0,"sessions":[{"id":"session","tags":null,"frame_ids":null}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	sessions, err := r.LoadAgentSessions()
	if err != nil || sessions[0].Tags == nil || sessions[0].FrameIDs == nil {
		t.Fatalf("sessions=%#v err=%v", sessions, err)
	}
}
