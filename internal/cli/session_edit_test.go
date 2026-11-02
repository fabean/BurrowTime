package cli

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fabean/BurrowTime/internal/store"
	"github.com/fabean/BurrowTime/internal/watson"
)

func TestSessionPickerFiltersAndNavigatesRecentSessions(t *testing.T) {
	s := editTestService(t)
	now := s.Now()
	stopA := now.Add(-3 * time.Hour).Unix()
	stopB := now.Add(-time.Hour).Unix()
	s.Frames = []store.Frame{
		{Start: now.Add(-4 * time.Hour).Unix(), Stop: &stopA, Project: "alpha", ID: "aaaaaaaaaaaaaaaa", Tags: []string{"A-1"}},
		{Start: now.Add(-2 * time.Hour).Unix(), Stop: &stopB, Project: "beta", ID: "bbbbbbbbbbbbbbbb", Tags: []string{"B-2"}},
		{Start: now.Unix(), Project: "running", ID: "current-primary"},
	}
	model := newSessionPickerModel(s, "")
	if len(model.filtered) != 2 || model.filtered[0].frame.Project != "beta" {
		t.Fatalf("picker should show completed sessions newest first: %#v", model.filtered)
	}
	model, _ = sessionPickerUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model, _ = sessionPickerUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("alpha")})
	if len(model.filtered) != 1 || model.filtered[0].frame.ID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("project filtering returned %#v", model.filtered)
	}
	model, _ = sessionPickerUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlU})
	model, _ = sessionPickerUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("B-2")})
	if len(model.filtered) != 1 || model.filtered[0].frame.Project != "beta" {
		t.Fatalf("tag filtering returned %#v", model.filtered)
	}
	model, _ = sessionPickerUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = sessionPickerUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.selected != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("selected reference=%q", model.selected)
	}
}

func TestEditableSessionsSkipsActiveAndAgentOwnedFrames(t *testing.T) {
	s := editTestService(t)
	stop := s.Now().Add(-time.Hour).Unix()
	s.Frames = []store.Frame{
		{Start: stop - 3600, Stop: &stop, Project: "ordinary", ID: "aaaaaaaaaaaaaaaa"},
		{Start: stop - 7200, Stop: &stop, Project: "agent", ID: "bbbbbbbbbbbbbbbb"},
		{Start: s.Now().Unix(), Project: "active", ID: "current-primary"},
	}
	s.AgentSessions = []store.AgentSession{{FrameIDs: []string{"bbbbbbbbbbbbbbbb"}}}
	options := editableSessions(s)
	if len(options) != 1 || options[0].frame.Project != "ordinary" {
		t.Fatalf("editable sessions=%#v", options)
	}
}

func TestSessionEditFormPrepopulatesEditsHistoricalTimesAndConfirms(t *testing.T) {
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.Local)
	start := time.Date(2026, time.October, 26, 9, 0, 0, 0, time.Local)
	stop := start.Add(90 * time.Minute)
	stopUnix := stop.Unix()
	frame := store.Frame{Start: start.Unix(), Stop: &stopUnix, Project: "RAF", ID: "aaaaaaaaaaaaaaaa", Tags: []string{"RAF-326"}}
	model := newSessionEditModel(frame, frame.ID, now)
	if model.project != "RAF" || strings.Join(model.tags, ",") != "RAF-326" || !model.start.Equal(start) || !model.stop.Equal(stop) {
		t.Fatalf("form values were not prepopulated: %#v", model)
	}
	model.field = editProject
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlU})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Redesign")})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	model.field = editTags
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlU})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("FR-889, QA")})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	model.field = editStart
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlU})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("09:30")})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if got := model.start.Format("2006-01-02 15:04:05"); got != "2026-10-26 09:30:00" {
		t.Fatalf("time-only historical start date=%s", got)
	}
	if !strings.Contains(model.View(), "1h 00m 00s") {
		t.Fatalf("duration did not recalculate: %s", model.View())
	}
	model.field = editSave
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if !model.confirm || !strings.Contains(model.View(), "Project: RAF → Redesign") {
		t.Fatalf("confirmation did not summarize changes: %s", model.View())
	}
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if !model.confirmed {
		t.Fatal("confirmation did not approve save")
	}
	proposal := model.proposal()
	if proposal.Project != "Redesign" || strings.Join(proposal.Tags, ",") != "FR-889,QA" || proposal.Stop.Sub(proposal.Start) != time.Hour {
		t.Fatalf("proposal=%#v", proposal)
	}
}

func TestSessionEditFormRejectsZeroDurationWithoutDroppingEdits(t *testing.T) {
	start := time.Date(2026, time.October, 26, 9, 0, 0, 0, time.Local)
	stopUnix := start.Add(time.Hour).Unix()
	frame := store.Frame{Start: start.Unix(), Stop: &stopUnix, Project: "RAF", ID: "aaaaaaaaaaaaaaaa"}
	model := newSessionEditModel(frame, frame.ID, time.Date(2026, time.November, 2, 12, 0, 0, 0, time.Local))
	model.stop = start
	model.project = "Redesign"
	model.field = editSave
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.confirm || model.errorText == "" || model.project != "Redesign" {
		t.Fatalf("invalid duration state=%#v", model)
	}
}

func TestSessionEditFormCanReplaceAndClearMessage(t *testing.T) {
	start := time.Date(2026, time.October, 26, 9, 0, 0, 0, time.Local)
	stopUnix := start.Add(time.Hour).Unix()
	frame := store.Frame{Start: start.Unix(), Stop: &stopUnix, Project: "RAF", ID: "aaaaaaaaaaaaaaaa", Message: "Original note"}
	model := newSessionEditModel(frame, frame.ID, time.Date(2026, time.November, 2, 12, 0, 0, 0, time.Local))
	model.field = editMessage
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlU})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Updated note")})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if proposal := model.proposal(); proposal.Message != "Updated note" || !proposal.MessageSet {
		t.Fatalf("replacement message proposal=%#v", proposal)
	}

	model = newSessionEditModel(frame, frame.ID, time.Date(2026, time.November, 2, 12, 0, 0, 0, time.Local))
	model.field = editMessage
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlU})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if proposal := model.proposal(); proposal.Message != "" || !proposal.MessageSet {
		t.Fatalf("cleared message proposal=%#v", proposal)
	}
}

func TestInteractiveEditNonTTYDoesNotModifySession(t *testing.T) {
	dir := t.TempDir()
	repo, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	stop := time.Now().Add(-time.Hour).Unix()
	frame := store.Frame{Start: stop - 3600, Stop: &stop, Project: "original", ID: "aaaaaaaaaaaaaaaa", Tags: []string{"T-1"}}
	if err := repo.SaveFrames([]store.Frame{frame}); err != nil {
		t.Fatal(err)
	}
	output, err := runBurrowTimeCommand(dir, "edit", "-i", frame.ID)
	if err == nil || !strings.Contains(err.Error(), "requires a terminal") {
		t.Fatalf("non-TTY interactive edit err=%v output=%q", err, output)
	}
	loaded, err := repo.LoadFrames()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Project != "original" || loaded[0].ID != frame.ID || len(loaded[0].Tags) != 1 {
		t.Fatalf("non-TTY edit modified session: %#v", loaded)
	}
}

func sessionPickerUpdate(model sessionPickerModel, msg tea.KeyMsg) (sessionPickerModel, tea.Cmd) {
	updated, command := model.Update(msg)
	return updated.(sessionPickerModel), command
}

func sessionEditUpdate(model sessionEditModel, msg tea.KeyMsg) (sessionEditModel, tea.Cmd) {
	updated, command := model.Update(msg)
	return updated.(sessionEditModel), command
}

func editTestService(t *testing.T) *watson.Service {
	t.Helper()
	s, err := watson.OpenData(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.October, 28, 16, 0, 0, 0, time.Local)
	s.Now = func() time.Time { return now }
	return s
}
