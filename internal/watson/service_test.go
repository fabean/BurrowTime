package watson

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fabean/BurrowTime/internal/store"
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

func TestAgentSessionUsesStandardTimerAndManualStopIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(3_000, 0)
	s.Now = func() time.Time { return now }
	session, duplicate, err := s.StartAgentSession(AgentStartOptions{
		Client: "codex", Project: "burrowtime", Task: "BT-42", IdempotencyKey: "turn-1", Lease: 10 * time.Minute,
	})
	if err != nil || duplicate {
		t.Fatalf("start session: %#v duplicate=%v err=%v", session, duplicate, err)
	}
	if session.TimerID != PrimaryTimerID || len(s.RunningTimers()) != 1 || s.State.Project != "burrowtime" {
		t.Fatalf("agent session did not create a standard timer: %#v %#v", session, s.RunningTimers())
	}
	duplicateSession, duplicate, err := s.StartAgentSession(AgentStartOptions{
		Client: "codex", Project: "burrowtime", Task: "BT-42", IdempotencyKey: "turn-1", Lease: 10 * time.Minute,
	})
	if err != nil || !duplicate || duplicateSession.ID != session.ID || len(s.RunningTimers()) != 1 {
		t.Fatalf("idempotent start created another timer: %#v duplicate=%v err=%v", duplicateSession, duplicate, err)
	}
	if _, _, err := s.StartAgentSession(AgentStartOptions{Client: "codex", Project: "other", Task: "OTHER-1", IdempotencyKey: "turn-1"}); err == nil {
		t.Fatal("idempotency key reuse for another task should fail")
	}

	now = time.Unix(3_120, 0)
	frame, err := s.Stop(nil)
	if err != nil {
		t.Fatal(err)
	}
	updated, _, err := s.AgentSession(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != store.AgentSessionManuallyStopped || len(updated.FrameIDs) != 1 || updated.FrameIDs[0] != frame.ID {
		t.Fatalf("manual stop did not reconcile session: %#v", updated)
	}
	stopped, alreadyStopped, err := s.StopAgentSession(session.ID)
	if err != nil || !alreadyStopped || stopped.ID != session.ID || len(s.Frames) != 1 {
		t.Fatalf("idempotent session stop: %#v already=%v err=%v", stopped, alreadyStopped, err)
	}
}

func TestAgentSessionPauseResumeProducesOrdinaryFrames(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(4_000, 0)
	s.Now = func() time.Time { return now }
	session, _, err := s.StartAgentSession(AgentStartOptions{Client: "claude", Project: "portal", Task: "WEB-9"})
	if err != nil {
		t.Fatal(err)
	}
	now = time.Unix(4_100, 0)
	paused, err := s.PauseAgentSession(session.ID)
	if err != nil || paused.Status != store.AgentSessionPaused || len(paused.FrameIDs) != 1 || s.HasRunningTimers() {
		t.Fatalf("pause: %#v err=%v timers=%#v", paused, err, s.RunningTimers())
	}
	now = time.Unix(4_200, 0)
	resumed, err := s.ResumeAgentSession(session.ID)
	if err != nil || !resumed.Running() || !s.HasRunningTimers() {
		t.Fatalf("resume: %#v err=%v", resumed, err)
	}
	now = time.Unix(4_300, 0)
	stopped, already, err := s.StopAgentSession(session.ID)
	if err != nil || already || stopped.Status != store.AgentSessionStopped || len(stopped.FrameIDs) != 2 {
		t.Fatalf("stop: %#v already=%v err=%v", stopped, already, err)
	}
	if len(s.Frames) != 2 || s.Frames[0].Project != "portal" || s.Frames[1].Project != "portal" {
		t.Fatalf("session did not produce standard frames: %#v", s.Frames)
	}
}

func TestAgentSessionPromotionKeepsTimerOwnership(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(5_000, 0)
	s.Now = func() time.Time { return now }
	first, _, _ := s.StartAgentSession(AgentStartOptions{Client: "codex", Project: "one", Task: "ONE-1"})
	second, _, _ := s.StartAgentSession(AgentStartOptions{Client: "cursor", Project: "two", Task: "TWO-2"})
	if second.TimerID == PrimaryTimerID {
		t.Fatal("second session should begin as a concurrent timer")
	}
	now = time.Unix(5_100, 0)
	if _, _, err := s.StopAgentSession(first.ID); err != nil {
		t.Fatal(err)
	}
	promoted, _, _ := s.AgentSession(second.ID)
	if promoted.TimerID != PrimaryTimerID || s.State.Project != "two" {
		t.Fatalf("promoted ownership was not updated: %#v state=%#v", promoted, s.State)
	}
	now = time.Unix(5_200, 0)
	if _, _, err := s.StopAgentSession(second.ID); err != nil {
		t.Fatal(err)
	}
	if s.HasRunningTimers() || len(s.Frames) != 2 {
		t.Fatalf("promoted session did not stop cleanly: %#v", s)
	}
}

func TestAgentSessionLeaseRecoveryStopsAtExpiry(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(6_000, 0)
	s.Now = func() time.Time { return now }
	session, _, err := s.StartAgentSession(AgentStartOptions{Client: "gemini", Project: "ops", Task: "OPS-3", Lease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	now = time.Unix(6_061, 0)
	recovered, err := s.RecoverExpiredAgentSessions()
	if err != nil || len(recovered) != 1 || recovered[0].Status != store.AgentSessionExpired {
		t.Fatalf("recover: %#v err=%v", recovered, err)
	}
	if len(s.Frames) != 1 || *s.Frames[0].Stop != 6_060 || s.HasRunningTimers() {
		t.Fatalf("lease recovery used wrong stop time: %#v", s)
	}
	stopped, already, err := s.StopAgentSession(session.ID)
	if err != nil || !already || stopped.Status != store.AgentSessionExpired {
		t.Fatalf("expired stop was not idempotent: %#v already=%v err=%v", stopped, already, err)
	}
}
