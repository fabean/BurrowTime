package cli

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fabean/BurrowTime/internal/store"
	"github.com/fabean/BurrowTime/internal/watson"
	"github.com/spf13/cobra"
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

func TestSessionEditCtrlSSavesFromAnySelectedField(t *testing.T) {
	start := time.Date(2026, time.October, 26, 9, 0, 0, 0, time.Local)
	stopUnix := start.Add(time.Hour).Unix()
	frame := store.Frame{Start: start.Unix(), Stop: &stopUnix, Project: "RAF", ID: "aaaaaaaaaaaaaaaa"}
	model := newSessionEditModel(frame, frame.ID, start.Add(2*time.Hour))
	if !strings.Contains(model.View(), "s/Ctrl+s") {
		t.Fatalf("form help does not advertise the save shortcuts: %s", model.View())
	}
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if !model.confirm || model.confirmed {
		t.Fatalf("s should open confirmation without skipping it: %#v", model)
	}
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlS})
	if !model.confirm || model.confirmed {
		t.Fatalf("Ctrl+S should open confirmation without skipping it: %#v", model)
	}
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if !model.confirmed {
		t.Fatal("confirmation Enter did not approve save")
	}

	model = newSessionEditModel(frame, frame.ID, start.Add(2*time.Hour))
	model.field = editProject
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlU})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Updated project")})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlS})
	if model.editing || !model.confirm || model.project != "Updated project" {
		t.Fatalf("Ctrl+S while editing should apply the field and review save: %#v", model)
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

func TestSessionPickerViewsPreselectionAndEmptySearch(t *testing.T) {
	s := editTestService(t)
	stop := s.Now().Add(-time.Hour).Unix()
	s.Frames = []store.Frame{
		{Start: stop - 3600, Stop: &stop, Project: "alpha", ID: "aaaaaaaaaaaaaaaa", Tags: []string{"ticket"}},
		{Start: stop - 1800, Stop: &stop, Project: "legacy"},
	}
	model := newSessionPickerModel(s, "aaaa")
	if model.filtered[model.cursor].frame.Project != "alpha" {
		t.Fatal("ID prefix was not preselected")
	}
	if model.Init() != nil {
		t.Fatal("session picker unexpectedly schedules work")
	}
	if view := model.View(); !strings.Contains(view, "alpha") || !strings.Contains(view, "#2") || !strings.Contains(view, "ticket") {
		t.Fatalf("picker missing session details: %s", view)
	}
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	model = updated.(sessionPickerModel)
	if model.width != 60 || model.height != 16 {
		t.Fatal("picker ignored resize")
	}
	model, _ = sessionPickerUpdate(model, tea.KeyMsg{Type: tea.KeyUp})
	model, _ = sessionPickerUpdate(model, tea.KeyMsg{Type: tea.KeyDown})
	model, _ = sessionPickerUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !strings.Contains(model.View(), "finish search") {
		t.Fatal("search controls missing")
	}
	model, _ = sessionPickerUpdate(model, tea.KeyMsg{Type: tea.KeySpace})
	model, _ = sessionPickerUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("missing")})
	model, _ = sessionPickerUpdate(model, tea.KeyMsg{Type: tea.KeyBackspace})
	model, _ = sessionPickerUpdate(model, tea.KeyMsg{Type: tea.KeyDelete})
	if got := string(model.query); got != " missi" {
		t.Fatalf("search editing=%q", got)
	}
	if len(model.filtered) != 0 || !strings.Contains(model.View(), "No completed editable sessions match") {
		t.Fatal("empty search state missing")
	}
	model, _ = sessionPickerUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	model, command := sessionPickerUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || model.selected != "" {
		t.Fatal("empty search selected a session")
	}
	model, command = sessionPickerUpdate(model, tea.KeyMsg{Type: tea.KeyEsc})
	if !model.canceled || command == nil {
		t.Fatal("escape did not cancel picker")
	}
}

func TestSessionEditLocationUsesTZAndFallsBack(t *testing.T) {
	now := time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)
	t.Setenv("TZ", "America/New_York")
	if got := sessionEditLocation(now).String(); got != "America/New_York" {
		t.Fatalf("location=%s", got)
	}
	t.Setenv("TZ", "not/a/zone")
	if got := sessionEditLocation(now); got != time.UTC {
		t.Fatalf("invalid zone did not fall back: %v", got)
	}
}

func TestSessionEditValidationAndInputCancellation(t *testing.T) {
	now := time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)
	stop := now.Add(-time.Hour).Unix()
	frame := store.Frame{Start: stop - 3600, Stop: &stop, Project: "alpha", Tags: []string{"ticket"}}
	base := newSessionEditModel(frame, "-1", now)
	if base.Init() != nil || base.hasChanges() {
		t.Fatal("new edit form should contain unchanged values")
	}
	if got := strings.Join(base.changeSummary(), ";"); got != "No field values changed." {
		t.Fatalf("unchanged summary=%q", got)
	}
	cases := []struct {
		name, errorText string
		change          func(*sessionEditModel)
	}{
		{name: "empty project", errorText: "Project cannot be empty", change: func(m *sessionEditModel) { m.project = " " }},
		{name: "future start", errorText: "Start time cannot be in the future", change: func(m *sessionEditModel) { m.start = now.Add(time.Hour); m.stop = now.Add(2 * time.Hour) }},
		{name: "future stop", errorText: "Stop time cannot be in the future", change: func(m *sessionEditModel) { m.stop = now.Add(time.Hour) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := base
			tc.change(&model)
			model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlS})
			if model.confirm || !strings.Contains(model.errorText, tc.errorText) {
				t.Fatalf("validation error=%q confirm=%v", model.errorText, model.confirm)
			}
		})
	}
	for _, field := range []sessionEditField{editProject, editStart, editStop} {
		model := base
		model.field = field
		model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
		model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlU})
		if field != editProject {
			model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("invalid")})
		}
		model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlS})
		if !model.editing || model.errorText == "" || model.confirm {
			t.Fatalf("invalid field %v did not remain editable: %#v", field, model)
		}
		if !strings.Contains(model.View(), "review save") {
			t.Fatal("editing controls missing")
		}
		model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEsc})
		if model.editing || model.canceled || model.errorText != "" {
			t.Fatal("escape while editing should keep original value")
		}
	}
	model := base
	model.field = editMessage
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("éx")})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyBackspace})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeySpace})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyDelete})
	if string(model.input) != "é" {
		t.Fatalf("Unicode editing=%q", string(model.input))
	}
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlC})
	if !model.canceled {
		t.Fatal("Ctrl+C did not cancel while editing")
	}
	model = base
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyUp})
	if model.field != editSave {
		t.Fatal("up should wrap to Save")
	}
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyDown})
	if model.field != editProject {
		t.Fatal("down should wrap to Project")
	}
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEsc})
	if !model.canceled {
		t.Fatal("escape did not cancel form")
	}
	if tags := splitEditTags(" "); len(tags) != 0 {
		t.Fatalf("empty tags=%v", tags)
	}
}

func TestSessionEditEndValueSummaryAndResize(t *testing.T) {
	now := time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)
	stop := now.Add(-time.Hour).Unix()
	frame := store.Frame{Start: stop - 3600, Stop: &stop, Project: "alpha", Message: "original"}
	model := newSessionEditModel(frame, "-1", now)
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 100})
	model = updated.(sessionEditModel)
	if model.width != 100 {
		t.Fatal("form ignored resize")
	}
	model.field = editStop
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlU})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("11:30")})
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.stop.Hour() != 11 || model.stop.Minute() != 30 || !model.hasChanges() {
		t.Fatal("end time edit was lost")
	}
	model.message = "updated"
	model.field = editSave
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlS})
	view := model.View()
	if !strings.Contains(view, "End:") || !strings.Contains(view, "Message: original → updated") {
		t.Fatalf("missing change summary: %s", view)
	}
	model, _ = sessionEditUpdate(model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.confirm || model.canceled {
		t.Fatal("escape from confirmation should return to form")
	}
	model.stop = model.start
	if !strings.Contains(model.View(), "Duration:") {
		t.Fatal("invalid duration view missing")
	}
}

func TestSessionEditFormRequiresTerminal(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&strings.Builder{})
	model := sessionEditModel{project: "original"}
	got, err := runSessionEditForm(cmd, model)
	if err == nil || !strings.Contains(err.Error(), "requires a terminal") || got.project != "original" {
		t.Fatalf("nonterminal form changed model or omitted error: project=%q err=%v", got.project, err)
	}
}
