package cli

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fabean/BurrowTime/internal/store"
	"github.com/fabean/BurrowTime/internal/watson"
)

func TestSessionWizardRecentProjectsAndTagSuggestions(t *testing.T) {
	s := wizardTestService(t)
	now := s.Now()
	alphaOldStop := now.Add(-3 * time.Hour).Unix()
	betaStop := now.Add(-2 * time.Hour).Unix()
	alphaRecentStop := now.Add(-time.Hour).Unix()
	s.Frames = []store.Frame{
		{Start: now.Add(-4 * time.Hour).Unix(), Stop: &alphaOldStop, Project: "alpha", Tags: []string{"ALPHA-1"}},
		{Start: now.Add(-3 * time.Hour).Unix(), Stop: &betaStop, Project: "beta", Tags: []string{"BETA-1"}},
		{Start: now.Add(-2 * time.Hour).Unix(), Stop: &alphaRecentStop, Project: "alpha", Tags: []string{"ALPHA-2", "ALPHA-1"}},
	}
	model := newSessionWizardModel(s, startWizard, sessionWizardDefaults{})
	if got := strings.Join(model.projects, ","); got != "alpha,beta" {
		t.Fatalf("recent project order: %s", got)
	}
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.project != "alpha" || model.step != wizardTags {
		t.Fatalf("project selection: %#v", model)
	}
	if got := strings.Join(model.projectTags, ","); got != "ALPHA-2,ALPHA-1" {
		t.Fatalf("project tag suggestions: %s", got)
	}
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyTab})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("new ticket, review")})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if got := strings.Join(model.tags, ","); got != "ALPHA-2,new ticket,review" {
		t.Fatalf("selected tags: %s", got)
	}
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.step != wizardMessage || !model.hasStart || !model.startTime.Equal(now) {
		t.Fatalf("Now default: %#v", model)
	}
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.step != wizardConfirm {
		t.Fatalf("empty optional message should advance to confirmation: %#v", model)
	}
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if !model.confirmed || model.result().Start != nil {
		t.Fatalf("confirmed start result: %#v", model.result())
	}
}

func TestSessionWizardEnterSelectsHighlightedExistingTag(t *testing.T) {
	s := wizardTestService(t)
	now := s.Now()
	stop := now.Add(-time.Hour).Unix()
	s.Frames = []store.Frame{{Start: now.Add(-2 * time.Hour).Unix(), Stop: &stop, Project: "alpha", Tags: []string{"A-1", "A-2"}}}
	model := newSessionWizardModel(s, startWizard, sessionWizardDefaults{})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.step != wizardTags {
		t.Fatalf("project selection did not open tag suggestions: %#v", model)
	}
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyDown})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.step != wizardTags || strings.Join(model.tags, ",") != "A-2" {
		t.Fatalf("Enter did not select the highlighted suggestion: %#v", model)
	}
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlN})
	if model.step != wizardStart {
		t.Fatalf("Ctrl+N did not finish the optional tag step: %#v", model)
	}
}

func TestSessionWizardCanCreateProjectAndCancelBeforeSaving(t *testing.T) {
	s := wizardTestService(t)
	model := newSessionWizardModel(s, startWizard, sessionWizardDefaults{})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("new project")})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlN})
	if model.project != "new project" || model.step != wizardTags {
		t.Fatalf("new project selection: %#v", model)
	}
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !model.canceled || model.confirmed {
		t.Fatalf("confirmation cancellation state: %#v", model)
	}
	if len(s.Frames) != 0 || s.State.Running() {
		t.Fatal("wizard cancellation changed session data")
	}
}

func TestSessionWizardRetriesInvalidTimeAndUsesHistoricalStartDateForEnd(t *testing.T) {
	s := wizardTestService(t)
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.Local)
	s.Now = func() time.Time { return now }
	model := newSessionWizardModel(s, addWizard, sessionWizardDefaults{Project: "alpha"})
	model.step = wizardStart
	model.project = "alpha"
	model.input = []rune("not a time")
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.step != wizardStart || model.errorText == "" {
		t.Fatalf("invalid start should stay editable: %#v", model)
	}
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlU})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2026-10-26 23:30:27")})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.step != wizardEnd || model.startTime.Second() != 27 {
		t.Fatalf("explicit start time: %#v", model)
	}
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("00:15")})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.step != wizardEnd || model.errorText == "" {
		t.Fatalf("same-day end before start should be rejected: %#v", model)
	}
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlU})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2026-10-27 00:15")})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.step != wizardMessage || !model.hasEnd || model.endTime.Format("2006-01-02 15:04:05") != "2026-10-27 00:15:00" {
		t.Fatalf("overnight end date: %#v", model)
	}
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("updated report")})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.step != wizardConfirm || model.result().Message != "updated report" {
		t.Fatalf("message should be included in summary result: %#v", model.result())
	}
	result := model.result()
	if result.Start == nil || result.End == nil || result.Start.Second() != 27 {
		t.Fatalf("historical result lost timestamps: %#v", result)
	}
}

func TestSessionWizardRetainsMinutePrecisionForAdjacentStartAlignment(t *testing.T) {
	s := wizardTestService(t)
	now := time.Date(2026, time.November, 2, 12, 31, 0, 0, time.Local)
	s.Now = func() time.Time { return now }
	model := newSessionWizardModel(s, startWizard, sessionWizardDefaults{Project: "alpha"})
	model.step = wizardStart
	model.input = []rune("12:30")
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter}) // Skip optional message.
	result := model.result()
	if !result.StartMinutePrecision || result.Start == nil {
		t.Fatalf("minute precision was lost from wizard result: %#v", result)
	}

	previousStop := time.Date(2026, time.November, 2, 12, 30, 37, 0, time.Local)
	stopUnix := previousStop.Unix()
	aligned := alignStartMinuteBoundary([]store.Frame{{Stop: &stopUnix}}, result.Start, result.StartMinutePrecision)
	if !aligned.Equal(previousStop) {
		t.Fatalf("same-minute overlap was not aligned: got %s, want %s", aligned, previousStop)
	}

	meaningfulOverlap := time.Date(2026, time.November, 2, 12, 29, 0, 0, time.Local)
	unchanged := alignStartMinuteBoundary([]store.Frame{{Stop: &stopUnix}}, &meaningfulOverlap, true)
	if !unchanged.Equal(meaningfulOverlap) {
		t.Fatalf("meaningful overlap was unexpectedly aligned: %s", unchanged)
	}

	withSeconds := "2026-11-02 12:30:15"
	model = newSessionWizardModel(s, startWizard, sessionWizardDefaults{Project: "alpha", Start: withSeconds})
	model.step = wizardStart
	model.input = []rune(withSeconds)
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.result().StartMinutePrecision {
		t.Fatal("explicit seconds should not be treated as minute precision")
	}
}

func TestInteractiveCommandsRequireTTYWithoutChangingData(t *testing.T) {
	for _, command := range [][]string{{"start", "-i"}, {"add", "--interactive"}} {
		dir := t.TempDir()
		output, err := runBurrowTimeCommand(dir, command...)
		if err == nil || !strings.Contains(err.Error(), "requires a terminal") {
			t.Fatalf("%v should reject non-TTY input, output=%q err=%v", command, output, err)
		}
		s, openErr := watson.OpenData(dir)
		if openErr != nil {
			t.Fatal(openErr)
		}
		if s.State.Running() || len(s.Frames) != 0 {
			t.Fatalf("%v changed data in non-TTY mode", command)
		}
	}
}

func wizardTestService(t *testing.T) *watson.Service {
	t.Helper()
	s, err := watson.OpenData(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.October, 28, 16, 0, 0, 0, time.Local)
	s.Now = func() time.Time { return now }
	return s
}

func wizardUpdate(model sessionWizardModel, msg tea.KeyMsg) (sessionWizardModel, tea.Cmd) {
	updated, command := model.Update(msg)
	return updated.(sessionWizardModel), command
}
