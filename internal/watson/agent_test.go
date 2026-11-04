package watson

import (
	"reflect"
	"testing"
	"time"

	"github.com/fabean/BurrowTime/internal/store"
)

func TestAgentStartValidationAndNormalization(t *testing.T) {
	tests := []struct {
		name    string
		options AgentStartOptions
	}{
		{"client", AgentStartOptions{Project: "p", Task: "TASK-1"}},
		{"project", AgentStartOptions{Client: "codex", Task: "TASK-1"}},
		{"task", AgentStartOptions{Client: "codex", Project: "p"}},
		{"lease", AgentStartOptions{Client: "codex", Project: "p", Task: "TASK-1", Lease: time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := testService(t)
			if _, _, err := s.StartAgentSession(tt.options); err == nil || s.HasRunningTimers() {
				t.Fatalf("invalid session accepted: err=%v timers=%#v", err, s.RunningTimers())
			}
		})
	}
	s, _ := testService(t)
	session, _, err := s.StartAgentSession(AgentStartOptions{Client: " CODEX ", Project: " p ", Task: "+TASK-1", Tags: []string{"+tag", " ", "tag"}, Owner: " owner ", Repository: " repo ", Branch: " branch "})
	if err != nil || session.Client != "codex" || session.Project != "p" || session.Task != "TASK-1" || session.LeaseSeconds != 1800 || session.Owner != "owner" || session.Repository != "repo" || session.Branch != "branch" || !reflect.DeepEqual(session.Tags, []string{"TASK-1", "tag"}) {
		t.Fatalf("session=%#v err=%v", session, err)
	}
}

func TestManualPrimaryStopPreservesPromotedAgentOwnership(t *testing.T) {
	for _, operation := range []string{"stop", "cancel"} {
		t.Run(operation, func(t *testing.T) {
			s, now := testService(t)
			first, _, err := s.StartAgentSession(AgentStartOptions{Client: "codex", Project: "first", Task: "ONE-1"})
			if err != nil {
				t.Fatal(err)
			}
			second, _, err := s.StartAgentSession(AgentStartOptions{Client: "cursor", Project: "second", Task: "TWO-2"})
			if err != nil {
				t.Fatal(err)
			}
			*now = now.Add(time.Minute)
			if operation == "stop" {
				if _, err := s.Stop(nil); err != nil {
					t.Fatal(err)
				}
			} else if _, err := s.Cancel(); err != nil {
				t.Fatal(err)
			}
			stopped, _, err := s.AgentSession(first.ID)
			if err != nil || stopped.Running() {
				t.Fatalf("first=%#v err=%v", stopped, err)
			}
			promoted, _, err := s.AgentSession(second.ID)
			if err != nil || !promoted.Running() || promoted.TimerID != PrimaryTimerID || len(promoted.FrameIDs) != 0 {
				t.Fatalf("promotion stopped another agent: %#v err=%v", promoted, err)
			}
			*now = now.Add(time.Minute)
			stopped, already, err := s.StopAgentSession(second.ID)
			if err != nil || already || stopped.Status != store.AgentSessionStopped {
				t.Fatalf("promoted stop=%#v already=%v err=%v", stopped, already, err)
			}
		})
	}
}

func TestAgentSessionLifecycleErrorsAndIdempotency(t *testing.T) {
	s, now := testService(t)
	session, _, err := s.StartAgentSession(AgentStartOptions{Client: "codex", Project: "p", Task: "TASK-1"})
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Second)
	heartbeat, err := s.HeartbeatAgentSession(session.ID[:7])
	if err != nil || heartbeat.HeartbeatAt != now.Unix() {
		t.Fatalf("heartbeat=%#v err=%v", heartbeat, err)
	}
	if _, err := s.ResumeAgentSession(session.ID); err != nil {
		t.Fatal(err)
	}
	paused, err := s.PauseAgentSession(session.ID)
	if err != nil || paused.Status != store.AgentSessionPaused {
		t.Fatalf("paused=%#v err=%v", paused, err)
	}
	if _, err := s.PauseAgentSession(session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.HeartbeatAgentSession(session.ID); err == nil {
		t.Fatal("paused heartbeat accepted")
	}
	stopped, already, err := s.StopAgentSession(session.ID)
	if err != nil || already || stopped.Status != store.AgentSessionStopped || stopped.StoppedAt == nil {
		t.Fatalf("paused stop=%#v already=%v err=%v", stopped, already, err)
	}
	if _, err := s.PauseAgentSession(session.ID); err == nil {
		t.Fatal("stopped pause accepted")
	}
	if _, err := s.ResumeAgentSession(session.ID); err == nil {
		t.Fatal("stopped resume accepted")
	}
	for _, ref := range []string{"", "missing"} {
		t.Run("lookup "+ref, func(t *testing.T) {
			if _, _, err := s.AgentSession(ref); err == nil {
				t.Fatal("bad lookup accepted")
			}
			if _, err := s.PauseAgentSession(ref); err == nil {
				t.Fatal("bad pause accepted")
			}
			if _, err := s.ResumeAgentSession(ref); err == nil {
				t.Fatal("bad resume accepted")
			}
			if _, _, err := s.StopAgentSession(ref); err == nil {
				t.Fatal("bad stop accepted")
			}
			if _, err := s.HeartbeatAgentSession(ref); err == nil {
				t.Fatal("bad heartbeat accepted")
			}
		})
	}
	s.AgentSessions = append(s.AgentSessions, store.AgentSession{ID: session.ID + "duplicate"})
	if _, _, err := s.AgentSession(session.ID); err == nil {
		t.Fatal("ambiguous agent ref accepted")
	}
}

func TestAgentHeartbeatExpiresAtLeaseBoundary(t *testing.T) {
	s, now := testService(t)
	session, _, err := s.StartAgentSession(AgentStartOptions{Client: "codex", Project: "p", Task: "TASK-1", Lease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Minute)
	if _, err := s.HeartbeatAgentSession(session.ID); err == nil {
		t.Fatal("expired heartbeat extended lease")
	}
	recovered, _, err := s.AgentSession(session.ID)
	if err != nil || recovered.Status != store.AgentSessionExpired || *recovered.StoppedAt != now.Unix() {
		t.Fatalf("recovered=%#v err=%v", recovered, err)
	}
}

func TestAgentPersistenceFailureRollsBackStartedTimer(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		name := "primary"
		if concurrent {
			name = "concurrent"
		}
		t.Run(name, func(t *testing.T) {
			s, _ := testService(t)
			if concurrent {
				if _, err := s.Start("human", nil, nil, true, false); err != nil {
					t.Fatal(err)
				}
			}
			blockServiceWrite(t, s, "agent_sessions")
			if _, _, err := s.StartAgentSession(AgentStartOptions{Client: "codex", Project: "p", Task: "TASK-1"}); err == nil {
				t.Fatal("metadata write unexpectedly succeeded")
			}
			if len(s.Concurrent) != 0 || len(s.AgentSessions) != 0 || s.State.Running() != concurrent {
				t.Fatalf("orphan timer left after failure: %#v", s.RunningTimers())
			}
		})
	}
}

func TestAgentResumePersistenceFailureRollsBackTimer(t *testing.T) {
	s, now := testService(t)
	session, _, err := s.StartAgentSession(AgentStartOptions{Client: "codex", Project: "p", Task: "TASK-1"})
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Second)
	if _, err := s.PauseAgentSession(session.ID); err != nil {
		t.Fatal(err)
	}
	blockServiceWrite(t, s, "agent_sessions")
	if _, err := s.ResumeAgentSession(session.ID); err == nil {
		t.Fatal("resume metadata write succeeded")
	}
	if s.HasRunningTimers() || s.AgentSessions[0].Status != store.AgentSessionPaused {
		t.Fatalf("orphan resumed timer: %#v", s)
	}
	if _, _, err := s.StopAgentSession(session.ID); err == nil {
		t.Fatal("paused stop write succeeded")
	}
}

func TestAgentHeartbeatPersistenceFailure(t *testing.T) {
	s, now := testService(t)
	session, _, err := s.StartAgentSession(AgentStartOptions{Client: "codex", Project: "p", Task: "TASK-1"})
	if err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Second)
	blockServiceWrite(t, s, "agent_sessions")
	if _, err := s.HeartbeatAgentSession(session.ID); err == nil {
		t.Fatal("heartbeat metadata write succeeded")
	}
	if s.AgentSessions[0].HeartbeatAt == now.Unix() {
		t.Fatal("failed heartbeat changed in-memory lease")
	}
}
