package watson

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fabean/BurrowTime/internal/store"
)

func testService(t *testing.T) (*Service, *time.Time) {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(10000, 0).UTC()
	s.Now = func() time.Time { return now }
	return s, &now
}

func TestSessionMessagesFollowStoppedAndPromotedTimers(t *testing.T) {
	for _, operation := range []string{"stop", "cancel"} {
		t.Run(operation, func(t *testing.T) {
			s, now := testService(t)
			if _, err := s.Start("first", nil, nil, true, false); err != nil {
				t.Fatal(err)
			}
			if err := s.SetTimerMessage(PrimaryTimerID, "first message"); err != nil {
				t.Fatal(err)
			}
			timer, err := s.StartConcurrent("second", nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SetTimerMessage(timer.ID, "second message"); err != nil {
				t.Fatal(err)
			}
			*now = now.Add(time.Minute)
			if operation == "stop" {
				f, err := s.Stop(nil)
				if err != nil || f.Message != "first message" {
					t.Fatalf("frame=%#v err=%v", f, err)
				}
			} else if _, err := s.Cancel(); err != nil {
				t.Fatal(err)
			}
			if got := s.RunningTimers(); len(got) != 1 || got[0].Project != "second" || got[0].Message != "second message" {
				t.Fatalf("promotion lost message: %#v", got)
			}
			if _, ok := s.Messages.Timers[timer.ID]; ok {
				t.Fatal("old concurrent message key remains")
			}
			reloaded, err := Open(s.Repo.Dir)
			if err != nil {
				t.Fatal(err)
			}
			reloaded.Now = s.Now
			if got := reloaded.RunningTimers(); len(got) != 1 || got[0].Message != "second message" {
				t.Fatalf("persisted promotion lost message: %#v", got)
			}
			*now = now.Add(time.Minute)
			if _, err := reloaded.Cancel(); err != nil {
				t.Fatal(err)
			}
			if _, err := reloaded.Start("third", nil, nil, true, false); err != nil {
				t.Fatal(err)
			}
			if got := reloaded.RunningTimers()[0].Message; got != "" {
				t.Fatalf("new timer inherited canceled message %q", got)
			}
		})
	}
}

func TestFrameMessagesHaveIndependentStableIDs(t *testing.T) {
	s, now := testService(t)
	s.Frames = []store.Frame{{Start: 1, Stop: ptr(2), Project: "first", IDNull: true}, {Start: 3, Stop: ptr(4), Project: "second"}}
	if err := s.Repo.SaveFrames(s.Frames); err != nil {
		t.Fatal(err)
	}
	first, err := s.SetFrameMessage("-2", "first message")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.SetFrameMessage("-1", "second message")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == "" || second.ID == "" || first.ID == second.ID || first.IDNull || second.IDNull {
		t.Fatalf("message frames need unique IDs: %#v %#v", first, second)
	}
	reloaded, err := Open(s.Repo.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Frames[0].Message != "first message" || reloaded.Frames[1].Message != "second message" {
		t.Fatalf("reloaded=%#v", reloaded.Frames)
	}
	if _, err := s.SetFrameMessage(first.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Messages.Frames[first.ID]; ok {
		t.Fatal("clear left message metadata")
	}
	if _, err := s.SetFrameMessage("missing", "x"); err == nil {
		t.Fatal("unknown frame accepted")
	}
	if err := s.SetTimerMessage("missing", "x"); err == nil {
		t.Fatal("unknown timer accepted")
	}
	if _, err := s.Start("timer", nil, nil, true, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTimerMessage(PrimaryTimerID, "message"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTimerMessage(PrimaryTimerID, ""); err != nil {
		t.Fatal(err)
	}
	timer, err := s.StartConcurrent("concurrent", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTimerMessage(timer.ID, "concurrent message"); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	frames, err := s.StopAll(nil)
	if err != nil || len(frames) != 2 || frames[1].Message != "concurrent message" || len(s.Messages.Timers) != 0 {
		t.Fatalf("stopped=%#v messages=%#v err=%v", frames, s.Messages, err)
	}
}

func TestStopAllValidationDoesNotConsumeMessages(t *testing.T) {
	s, now := testService(t)
	if _, err := s.Start("first", nil, nil, true, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTimerMessage(PrimaryTimerID, "keep"); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Minute)
	if _, err := s.StartConcurrent("later", nil, nil); err != nil {
		t.Fatal(err)
	}
	stop := now.Add(-90 * time.Second)
	if _, err := s.StopAll(&stop); err == nil {
		t.Fatal("stop before concurrent start accepted")
	}
	if s.Messages.Timers[PrimaryTimerID] != "keep" || len(s.Messages.Frames) != 0 || len(s.Frames) != 0 {
		t.Fatalf("failed validation consumed messages: %#v", s.Messages)
	}
}

func TestServiceFrameEditingAndHistory(t *testing.T) {
	s, now := testService(t)
	if err := s.Config.Set("default_tags", "p", "default"); err != nil {
		t.Fatal(err)
	}
	f, err := s.Add("p", []string{"explicit"}, now.Add(-2*time.Minute), now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.Tags, []string{"explicit", "default"}) {
		t.Fatalf("tags=%v", f.Tags)
	}
	if _, err := s.SetFrameMessage(f.ID, "preserve"); err != nil {
		t.Fatal(err)
	}
	edited, err := s.EditFrame(f.ID, "changed", []string{"edited"}, now.Add(-90*time.Second), now.Add(-30*time.Second))
	if err != nil || edited.ID != f.ID || edited.Message != "preserve" {
		t.Fatalf("edited=%#v err=%v", edited, err)
	}
	tags := []string{"edited"}
	if _, err := s.EditFrame(f.ID, "changed", tags, now.Add(-90*time.Second), now.Add(-30*time.Second)); err != nil {
		t.Fatal(err)
	}
	tags[0] = "mutated"
	if s.Frames[0].Tags[0] != "edited" {
		t.Fatal("edit retained caller tags")
	}
	if err := s.Rename("project", "changed", "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := s.Rename("tag", "edited", "renamed-tag"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Projects(), []string{"renamed"}) || !reflect.DeepEqual(s.Tags(), []string{"renamed-tag"}) {
		t.Fatalf("projects=%v tags=%v", s.Projects(), s.Tags())
	}
	if err := s.Rename("project", "missing", "x"); err == nil {
		t.Fatal("missing project accepted")
	}
	if err := s.Rename("bogus", "renamed", "x"); err == nil {
		t.Fatal("invalid rename type accepted")
	}
	if _, _, err := s.Lookup("-2"); err == nil {
		t.Fatal("out-of-range index accepted")
	}
	removed, err := s.Remove("-1")
	if err != nil || removed.ID != f.ID || len(s.Frames) != 0 || len(s.Messages.Frames) != 0 {
		t.Fatalf("removed=%#v err=%v", removed, err)
	}
	if _, err := s.Remove("missing"); err == nil {
		t.Fatal("unknown remove accepted")
	}
}

func TestServiceValidatesSessionTimes(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Service, time.Time) error
	}{
		{"blank start", func(s *Service, n time.Time) error { _, e := s.Start(" ", nil, nil, true, false); return e }},
		{"future start", func(s *Service, n time.Time) error {
			at := n.Add(time.Minute)
			_, e := s.Start("p", nil, &at, true, false)
			return e
		}},
		{"no gap without history", func(s *Service, n time.Time) error { _, e := s.Start("p", nil, nil, false, false); return e }},
		{"blank concurrent", func(s *Service, n time.Time) error { _, e := s.StartConcurrent(" ", nil, nil); return e }},
		{"future concurrent", func(s *Service, n time.Time) error {
			at := n.Add(time.Minute)
			_, e := s.StartConcurrent("p", nil, &at)
			return e
		}},
		{"blank add", func(s *Service, n time.Time) error { _, e := s.Add(" ", nil, n, n); return e }},
		{"reversed add", func(s *Service, n time.Time) error { _, e := s.Add("p", nil, n, n.Add(-time.Minute)); return e }},
		{"stop without timer", func(s *Service, n time.Time) error { _, e := s.Stop(nil); return e }},
		{"stop all without timer", func(s *Service, n time.Time) error { _, e := s.StopAll(nil); return e }},
		{"cancel without timer", func(s *Service, n time.Time) error { _, e := s.Cancel(); return e }},
		{"edit without timer", func(s *Service, n time.Time) error { _, e := s.EditCurrent("p", nil, n); return e }},
		{"missing edit", func(s *Service, n time.Time) error { _, e := s.EditFrame("missing", "p", nil, n, n); return e }},
		{"empty timer ref", func(s *Service, n time.Time) error { _, e := s.StopTimer("", nil); return e }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, now := testService(t)
			if err := tt.run(s, *now); err == nil {
				t.Fatal("invalid operation accepted")
			}
		})
	}
	s, now := testService(t)
	if _, err := s.Start("p", nil, nil, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start("again", nil, nil, true, false); err == nil {
		t.Fatal("duplicate primary accepted")
	}
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)
	for _, at := range []time.Time{past, future} {
		if _, err := s.Stop(&at); err == nil {
			t.Fatalf("invalid stop %v accepted", at)
		}
	}
	if _, err := s.EditCurrent("p", nil, future); err == nil {
		t.Fatal("future edit accepted")
	}
	if _, err := s.EditCurrent("renamed", []string{"tag"}, past); err != nil {
		t.Fatal(err)
	}
	s.Concurrent = []store.ActiveTimer{{ID: "abc1", Project: "one", Start: 1}, {ID: "abc2", Project: "two", Start: 1}}
	if _, err := s.StopTimer("abc", nil); err == nil {
		t.Fatal("ambiguous timer accepted")
	}
	s.Frames = []store.Frame{{Start: 1, Stop: ptr(2), Project: "p", ID: "frame"}}
	for _, tt := range []struct {
		name        string
		start, stop time.Time
	}{{"reversed", *now, past}, {"future start", future, future}, {"future stop", past, future}} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.EditFrame("frame", "p", nil, tt.start, tt.stop); err == nil {
				t.Fatal("invalid edit accepted")
			}
		})
	}
}

func TestStartUsesPreviousEndAndDefaultTags(t *testing.T) {
	s, now := testService(t)
	s.Frames = []store.Frame{{Start: 1, Stop: ptr(2), Project: "old", ID: "old"}}
	if err := s.Config.Set("default_tags", "new", "default explicit"); err != nil {
		t.Fatal(err)
	}
	at := time.Unix(1, 0)
	if _, err := s.Start("new", nil, &at, true, false); err == nil {
		t.Fatal("start before previous end accepted")
	}
	state, err := s.Start("new", []string{"explicit"}, nil, false, false)
	if err != nil || state.Start != 2 || !reflect.DeepEqual(state.Tags, []string{"explicit", "default"}) {
		t.Fatalf("state=%#v err=%v", state, err)
	}
	if _, err := s.Cancel(); err != nil {
		t.Fatal(err)
	}
	state, err = s.Start("new", []string{"only"}, now, true, true)
	if err != nil || !reflect.DeepEqual(state.Tags, []string{"only"}) {
		t.Fatalf("restart=%#v err=%v", state, err)
	}
}

func TestMergeIgnoresLocalMessageMetadata(t *testing.T) {
	s, now := testService(t)
	f, err := s.Add("p", nil, now.Add(-time.Minute), *now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetFrameMessage(f.ID, "local message"); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal([]store.Frame{f})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "incoming")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	conflicts, merging, err := s.MergeReport(path)
	if err != nil || len(conflicts) != 0 || len(merging) != 0 {
		t.Fatalf("metadata caused false conflict: conflicts=%#v merging=%#v err=%v", conflicts, merging, err)
	}
	f.Project = "remote"
	data, err = json.Marshal([]store.Frame{f})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	conflicts, _, err = s.MergeReport(path)
	if err != nil || len(conflicts) != 1 || conflicts[0].Message != "local message" {
		t.Fatalf("conflict loses metadata: %#v err=%v", conflicts, err)
	}
	if err := s.ApplyMerge(map[string]store.Frame{f.ID: conflicts[0]}, nil); err != nil {
		t.Fatal(err)
	}
	if s.Frames[0].Message != "local message" {
		t.Fatal("merge lost message")
	}
	if _, _, err := s.MergeReport(filepath.Join(path, "missing")); err == nil {
		t.Fatal("missing import accepted")
	}
}

func TestOpenRejectsInvalidCompanionFiles(t *testing.T) {
	for _, name := range []string{"frames", "state", "active_timers", "agent_sessions", "session_messages", "config"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, name), []byte("invalid"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(dir); err == nil {
				t.Fatalf("invalid %s accepted", name)
			}
		})
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "frames"), []byte(`[[1,2,"p","legacy"]]`), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := OpenData(dir)
	if err != nil || s.Frames[0].UpdatedAtMissing || s.Frames[0].UpdatedAt == 0 {
		t.Fatalf("legacy normalization=%#v err=%v", s, err)
	}
	if shortID("abcdefghijk") != "abcdefg" || shortID("abc") != "abc" {
		t.Fatal("short ID mismatch")
	}
}

func blockServiceWrite(t *testing.T, s *Service, name string) {
	t.Helper()
	path := filepath.Join(s.Repo.Dir, name+".bak")
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Repo.Dir, name), []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "keep"), nil, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestServiceWriteErrorsAreReturned(t *testing.T) {
	tests := []struct {
		name, file string
		prepare    func(*Service, time.Time)
		run        func(*Service, time.Time) error
	}{
		{"start", "state", nil, func(s *Service, n time.Time) error { _, e := s.Start("p", nil, nil, true, false); return e }},
		{"concurrent primary", "state", nil, func(s *Service, n time.Time) error { _, e := s.StartConcurrent("p", nil, nil); return e }},
		{"concurrent secondary", "active_timers", func(s *Service, n time.Time) { s.State = store.State{Project: "p", Start: 1} }, func(s *Service, n time.Time) error { _, e := s.StartConcurrent("other", nil, nil); return e }},
		{"add", "frames", nil, func(s *Service, n time.Time) error { _, e := s.Add("p", nil, n, n); return e }},
		{"edit frame", "frames", func(s *Service, n time.Time) { s.Frames = []store.Frame{{ID: "frame", Stop: ptr(2)}} }, func(s *Service, n time.Time) error { _, e := s.EditFrame("frame", "p", nil, n, n); return e }},
		{"remove", "frames", func(s *Service, n time.Time) { s.Frames = []store.Frame{{ID: "frame", Stop: ptr(2)}} }, func(s *Service, n time.Time) error { _, e := s.Remove("frame"); return e }},
		{"rename", "frames", func(s *Service, n time.Time) { s.Frames = []store.Frame{{ID: "frame", Project: "p", Stop: ptr(2)}} }, func(s *Service, n time.Time) error { return s.Rename("project", "p", "new") }},
		{"frame message", "session_messages", func(s *Service, n time.Time) { s.Frames = []store.Frame{{ID: "frame", Stop: ptr(2)}} }, func(s *Service, n time.Time) error { _, e := s.SetFrameMessage("frame", "message"); return e }},
		{"timer message", "session_messages", func(s *Service, n time.Time) { s.State = store.State{Project: "p", Start: 1} }, func(s *Service, n time.Time) error { return s.SetTimerMessage(PrimaryTimerID, "message") }},
		{"edit current", "state", func(s *Service, n time.Time) { s.State = store.State{Project: "p", Start: 1} }, func(s *Service, n time.Time) error { _, e := s.EditCurrent("other", nil, n); return e }},
		{"stop frames", "frames", func(s *Service, n time.Time) { s.State = store.State{Project: "p", Start: 1} }, func(s *Service, n time.Time) error { _, e := s.Stop(nil); return e }},
		{"stop state", "state", func(s *Service, n time.Time) { s.State = store.State{Project: "p", Start: 1} }, func(s *Service, n time.Time) error { _, e := s.Stop(nil); return e }},
		{"stop messages", "session_messages", func(s *Service, n time.Time) { s.State = store.State{Project: "p", Start: 1} }, func(s *Service, n time.Time) error { _, e := s.Stop(nil); return e }},
		{"cancel state", "state", func(s *Service, n time.Time) { s.State = store.State{Project: "p", Start: 1} }, func(s *Service, n time.Time) error { _, e := s.Cancel(); return e }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, n := testService(t)
			if tt.prepare != nil {
				tt.prepare(s, *n)
			}
			blockServiceWrite(t, s, tt.file)
			if err := tt.run(s, *n); err == nil || !strings.Contains(err.Error(), tt.file) {
				t.Fatalf("write error=%v", err)
			}
		})
	}
}
