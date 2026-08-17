package watson

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/josh/burrowtime/internal/store"
)

func TestStartStopUsesWatsonDirectoryWithoutMigration(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("[default_tags]\napollo = inherited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(10_000, 0)
	s.Now = func() time.Time { return now }
	state, err := s.Start("apollo", []string{"manual"}, nil, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Tags) != 2 || state.Tags[1] != "inherited" {
		t.Fatalf("default tags missing: %#v", state.Tags)
	}
	now = time.Unix(10_600, 0)
	frame, err := s.Stop(nil)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Start != 10_000 || *frame.Stop != 10_600 || len(frame.ID) != 32 {
		t.Fatalf("bad stopped frame: %#v", frame)
	}
	repo, _ := store.New(dir)
	loaded, err := repo.LoadFrames()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Project != "apollo" {
		t.Fatalf("persisted frame: %#v", loaded)
	}
	st, err := repo.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if st.Running() {
		t.Fatalf("state was not cleared: %#v", st)
	}
	if _, err := os.Stat(filepath.Join(dir, "active_timers")); !os.IsNotExist(err) {
		t.Fatalf("ordinary Watson-compatible start/stop created companion state: %v", err)
	}
}

func TestPrefixAndNegativeLookup(t *testing.T) {
	s := &Service{Frames: []store.Frame{{ID: "aaa111", Project: "first"}, {ID: "aaa222", Project: "second"}}}
	if f, _, err := s.Lookup("aaa"); err != nil || f.Project != "first" {
		t.Fatalf("prefix semantics differ: %#v %v", f, err)
	}
	if f, _, err := s.Lookup("-1"); err != nil || f.Project != "second" {
		t.Fatalf("negative lookup failed: %#v %v", f, err)
	}
}

func TestConcurrentTimersStopIndividuallyAndPromoteToWatsonState(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_000, 0)
	s.Now = func() time.Time { return now }
	if _, err := s.Start("primary", []string{"one"}, nil, true, false); err != nil {
		t.Fatal(err)
	}
	now = time.Unix(1_100, 0)
	second, err := s.StartConcurrent("second", []string{"two"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.RunningTimers()) != 2 || second.Primary {
		t.Fatalf("running timers: %#v", s.RunningTimers())
	}

	now = time.Unix(1_200, 0)
	frame, err := s.StopTimer(second.ID[:7], nil)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Project != "second" || s.State.Project != "primary" || len(s.Concurrent) != 0 {
		t.Fatalf("individual stop produced frame=%#v state=%#v concurrent=%#v", frame, s.State, s.Concurrent)
	}

	now = time.Unix(1_300, 0)
	third, err := s.StartConcurrent("third", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	now = time.Unix(1_400, 0)
	if _, err := s.StopTimer(PrimaryTimerID, nil); err != nil {
		t.Fatal(err)
	}
	if s.State.Project != third.Project || len(s.Concurrent) != 0 {
		t.Fatalf("concurrent timer was not promoted: state=%#v concurrent=%#v", s.State, s.Concurrent)
	}
	reloaded, err := OpenData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.State.Project != "third" || len(reloaded.Concurrent) != 0 {
		t.Fatalf("promotion was not persisted: %#v", reloaded)
	}
}

func TestStopAllUsesOneTimestampAndClearsAllState(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(2_000, 0)
	s.Now = func() time.Time { return now }
	if _, err := s.Start("one", nil, nil, true, false); err != nil {
		t.Fatal(err)
	}
	now = time.Unix(2_100, 0)
	if _, err := s.StartConcurrent("two", nil, nil); err != nil {
		t.Fatal(err)
	}
	now = time.Unix(2_200, 0)
	frames, err := s.StopAll(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || *frames[0].Stop != 2_200 || *frames[1].Stop != 2_200 {
		t.Fatalf("stopped frames: %#v", frames)
	}
	if s.HasRunningTimers() || len(s.Frames) != 2 {
		t.Fatalf("state was not cleared: %#v", s)
	}
}
