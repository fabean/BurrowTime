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
