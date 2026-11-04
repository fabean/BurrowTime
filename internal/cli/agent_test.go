package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fabean/BurrowTime/internal/store"
	"github.com/fabean/BurrowTime/internal/watson"
)

func TestAgentCLIUsesStandardTimerAndManualStopReconciles(t *testing.T) {
	dir := t.TempDir()
	output, err := runBurrowTimeCommand(dir, "agent", "start", "--client", "codex", "--project", "sema", "--task", "+SEMA-158", "--idempotency-key", "chat-1:SEMA-158", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var started agentSessionResult
	if err := json.Unmarshal([]byte(output), &started); err != nil {
		t.Fatalf("decode start: %v\n%s", err, output)
	}
	if started.Session.Project != "sema" || started.Session.Task != "SEMA-158" || started.Session.TimerID != watson.PrimaryTimerID {
		t.Fatalf("started session: %#v", started.Session)
	}

	status, err := runBurrowTimeCommand(dir, "status")
	if err != nil || !strings.Contains(status, "Project sema") || !strings.Contains(status, "SEMA-158") {
		t.Fatalf("standard status=%q err=%v", status, err)
	}
	if _, err := runBurrowTimeCommand(dir, "stop"); err != nil {
		t.Fatalf("manual stop: %v", err)
	}

	output, err = runBurrowTimeCommand(dir, "agent", "stop", "--session", started.Session.ID, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var stopped agentSessionResult
	if err := json.Unmarshal([]byte(output), &stopped); err != nil {
		t.Fatal(err)
	}
	if stopped.Session.Status != store.AgentSessionManuallyStopped || !stopped.AlreadyStopped {
		t.Fatalf("reconciled session: %#v", stopped)
	}
}

func TestAgentReportRejectsUnknownStatus(t *testing.T) {
	_, err := runBurrowTimeCommand(t.TempDir(), "agent", "report", "--status", "mystery")
	if err == nil || !strings.Contains(err.Error(), "unknown agent session status") {
		t.Fatalf("expected status validation error, got %v", err)
	}
}

func TestAgentCLICompleteLifecycle(t *testing.T) {
	dir := t.TempDir()
	startArgs := []string{
		"agent", "start", "--client", "codex", "--project", "project", "--task", "T-1",
		"--idempotency-key", "task-1", "--lease", "5m", "--repository", "repo", "--branch", "main", "-m", "initial",
	}
	out, err := runBurrowTimeCommand(dir, append(startArgs, "--json")...)
	if err != nil {
		t.Fatal(err)
	}
	var started agentSessionResult
	if err := json.Unmarshal([]byte(out), &started); err != nil {
		t.Fatal(err)
	}
	if out, err := runBurrowTimeCommand(dir, startArgs...); err != nil || !strings.Contains(out, "Reused agent session") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	for _, command := range []string{"heartbeat", "pause", "resume"} {
		out, err := runBurrowTimeCommand(dir, "agent", command, "--session", started.Session.ID)
		if err != nil || !strings.Contains(out, "Agent session") {
			t.Fatalf("%s: out=%q err=%v", command, out, err)
		}
	}
	for _, args := range [][]string{
		{"agent", "status"},
		{"agent", "status", "--active", "--json"},
		{"agent", "status", "--session", started.Session.ID, "--json"},
		{"agent", "report"},
		{"agent", "report", "--client", "missing"},
		{"agent", "report", "--project", "missing", "--json"},
		{"agent", "report", "--status", "paused", "--json"},
		{"agent", "recover"},
		{"agent", "recover", "--json"},
	} {
		if out, err := runBurrowTimeCommand(dir, args...); err != nil || out == "" {
			t.Fatalf("%v: out=%q err=%v", args, out, err)
		}
	}
	if out, err := runBurrowTimeCommand(dir, "agent", "stop", "--session", started.Session.ID, "-m", "done"); err != nil || !strings.Contains(out, "stopped") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if _, err := runBurrowTimeCommand(dir, "start", "human", "-m", "human message"); err != nil {
		t.Fatal(err)
	}
	if _, err := runBurrowTimeCommand(dir, "agent", "stop", "--session", started.Session.ID, "-m", "should not overwrite"); err != nil {
		t.Fatal(err)
	}
	s, err := watson.OpenData(dir)
	if err != nil || s.Messages.Timers[watson.PrimaryTimerID] != "human message" {
		t.Fatalf("service=%+v err=%v", s, err)
	}
}

func TestAgentStopMessageRejectsAmbiguousSessionWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	s, err := watson.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range []string{"T-1", "T-2"} {
		if _, _, err := s.StartAgentSession(watson.AgentStartOptions{Client: "codex", Project: "project", Task: task}); err != nil {
			t.Fatal(err)
		}
	}
	s.AgentSessions[0].ID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1"
	s.AgentSessions[1].ID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa2"
	if err := s.Repo.SaveAgentSessions(s.AgentSessions); err != nil {
		t.Fatal(err)
	}
	if _, err := runBurrowTimeCommand(dir, "agent", "stop", "--session", "aaaa", "-m", "wrong"); err == nil {
		t.Fatal("accepted ambiguous session")
	}
	loaded, err := watson.OpenData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages.Timers) != 0 || len(loaded.RunningTimers()) != 2 {
		t.Fatalf("mutated ambiguous sessions: %+v", loaded)
	}
}

func TestAgentCommandValidationAndReadErrors(t *testing.T) {
	for _, args := range [][]string{
		{"agent", "start", "--lease", "bad"},
		{"agent", "status", "--session", "missing"},
		{"agent", "heartbeat", "--session", "missing"},
		{"agent", "pause", "--session", "missing"},
		{"agent", "resume", "--session", "missing"},
		{"agent", "stop", "--session", "missing", "-m", "message"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if _, err := runBurrowTimeCommand(t.TempDir(), args...); err == nil {
				t.Fatal("accepted invalid command")
			}
		})
	}
	for _, command := range []string{"start", "status", "report", "recover", "stop"} {
		t.Run(command+" corrupt state", func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "frames"), []byte("bad"), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"agent", command}
			if command == "stop" {
				args = append(args, "--session", "missing")
			}
			if _, err := runBurrowTimeCommand(dir, args...); err == nil {
				t.Fatal("accepted corrupt storage")
			}
		})
	}
}

func TestAgentReportGroupsStandardFramesBySession(t *testing.T) {
	dir := t.TempDir()
	s, err := watson.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(7_000, 0)
	s.Now = func() time.Time { return now }
	session, _, err := s.StartAgentSession(watson.AgentStartOptions{Client: "claude", Project: "docs", Task: "DOC-2"})
	if err != nil {
		t.Fatal(err)
	}
	now = time.Unix(7_125, 0)
	if _, _, err := s.StopAgentSession(session.ID); err != nil {
		t.Fatal(err)
	}

	output, err := runBurrowTimeCommand(dir, "agent", "report", "--client", "claude", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []agentReportRow
	if err := json.Unmarshal([]byte(output), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Session.ID != session.ID || rows[0].DurationSeconds != 125 {
		t.Fatalf("report rows: %#v", rows)
	}
}

func TestAgentCLIUsesRepositoryDefaults(t *testing.T) {
	workingDirectory := t.TempDir()
	dataDirectory := t.TempDir()
	config := "[agent]\nproject = \"portal\"\ntask = \"WEB-7\"\nrepository = \"portal-app\"\nlease = \"12m\"\n"
	if err := os.WriteFile(filepath.Join(workingDirectory, ".burrowtime.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalDirectory) })

	output, err := runBurrowTimeCommand(dataDirectory, "agent", "start", "--client", "gemini", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var result agentSessionResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatal(err)
	}
	if result.Session.Project != "portal" || result.Session.Task != "WEB-7" || result.Session.Repository != "portal-app" || result.Session.LeaseSeconds != 720 {
		t.Fatalf("session defaults: %#v", result.Session)
	}
}

func TestStandardStatusRecoversExpiredAgentLease(t *testing.T) {
	dir := t.TempDir()
	s, err := watson.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return time.Unix(1_000, 0) }
	session, _, err := s.StartAgentSession(watson.AgentStartOptions{Client: "opencode", Project: "old", Task: "OLD-1", Lease: time.Minute})
	if err != nil {
		t.Fatal(err)
	}

	output, err := runBurrowTimeCommand(dir, "status")
	if err != nil || !strings.Contains(output, "No project started") {
		t.Fatalf("status=%q err=%v", output, err)
	}
	reloaded, err := watson.OpenData(dir)
	if err != nil {
		t.Fatal(err)
	}
	recovered, _, err := reloaded.AgentSession(session.ID)
	if err != nil || recovered.Status != store.AgentSessionExpired || len(reloaded.Frames) != 1 || *reloaded.Frames[0].Stop != 1_060 {
		t.Fatalf("recovered=%#v frames=%#v err=%v", recovered, reloaded.Frames, err)
	}
}

func TestCapabilitiesJSONAdvertisesPortableTargets(t *testing.T) {
	output, err := runBurrowTimeCommand(t.TempDir(), "capabilities", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var capabilities capabilityDocument
	if err := json.Unmarshal([]byte(output), &capabilities); err != nil {
		t.Fatal(err)
	}
	if capabilities.AgentProtocol != 1 || !capabilities.Features["agent_sessions"] || len(capabilities.SkillTargets) != 6 {
		t.Fatalf("capabilities: %#v", capabilities)
	}
}
