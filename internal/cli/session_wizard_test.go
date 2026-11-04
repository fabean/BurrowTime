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

func TestSessionWizardSuggestionsExcludeSelectedTags(t *testing.T) {
	model := newSessionWizardModel(wizardTestService(t), startWizard, sessionWizardDefaults{Project: "alpha"})
	model.step = wizardTags
	model.projectTags = []string{"A-1", "A-2"}
	model.input = nil
	for _, want := range []string{"A-1", "A-2"} {
		model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
		if !containsString(model.tags, want) || model.step != wizardTags {
			t.Fatalf("Enter should select the next available tag %q: tags=%v step=%v", want, model.tags, model.step)
		}
	}
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.step != wizardStart || len(model.tags) != 2 {
		t.Fatalf("exhausted suggestions should continue: tags=%v step=%v", model.tags, model.step)
	}
	model.input = []rune("a-")
	if tags := model.filteredTags(); len(tags) != 0 {
		t.Fatalf("selected tags returned during search: %v", tags)
	}
}

func TestSessionWizardProjectSearchAndNewProject(t *testing.T) {
	cases := []struct {
		name, input, want string
		down              bool
	}{
		{name: "empty chooses highlighted", down: true, want: "beta"},
		{name: "case insensitive exact", input: "BETA", want: "beta"},
		{name: "partial match", input: "alp", want: "alpha"},
		{name: "new name", input: "gamma", want: "gamma"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := newSessionWizardModel(wizardTestService(t), startWizard, sessionWizardDefaults{})
			model.projects = []string{"alpha", "beta"}
			model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tc.input)})
			if tc.down {
				model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyDown})
			}
			model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
			if model.project != tc.want || model.step != wizardTags {
				t.Fatalf("project=%q step=%v, want %q and tags", model.project, model.step, tc.want)
			}
		})
	}
	model := newSessionWizardModel(wizardTestService(t), startWizard, sessionWizardDefaults{})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.errorText == "" || model.step != wizardProject {
		t.Fatal("empty project was accepted")
	}
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlN})
	if model.errorText == "" || model.step != wizardProject {
		t.Fatal("new empty project was accepted")
	}
	if model.Init() != nil {
		t.Fatal("wizard unexpectedly schedules work")
	}
}

func TestSessionWizardEditingAndBacktracking(t *testing.T) {
	model := newSessionWizardModel(wizardTestService(t), addWizard, sessionWizardDefaults{
		Project: "alpha", Tags: []string{"ticket"}, Start: "09:00", End: "10:00", Message: "note",
	})
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = updated.(sessionWizardModel)
	if model.width != 120 || model.height != 40 {
		t.Fatal("window size was not retained")
	}
	model.input = []rune("é")
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeySpace})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("work")})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyBackspace})
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyDelete})
	if string(model.input) != "é wo" {
		t.Fatalf("edited input=%q", string(model.input))
	}
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlU})
	if len(model.input) != 0 || model.errorText != "" {
		t.Fatal("clear did not clear input and validation")
	}
	cases := []struct {
		name       string
		from, want sessionWizardStep
		text       string
	}{
		{name: "tags", from: wizardTags, want: wizardProject, text: "alpha"},
		{name: "start", from: wizardStart, want: wizardTags, text: "ticket"},
		{name: "end", from: wizardEnd, want: wizardStart, text: "09:00"},
		{name: "message", from: wizardMessage, want: wizardEnd, text: "10:00"},
		{name: "confirmation", from: wizardConfirm, want: wizardMessage, text: "note"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			current := model
			current.step, current.errorText = tc.from, "old error"
			current, _ = wizardUpdate(current, tea.KeyMsg{Type: tea.KeyEsc})
			if current.step != tc.want || string(current.input) != tc.text || current.errorText != "" {
				t.Fatalf("backtracking produced step=%v input=%q error=%q", current.step, string(current.input), current.errorText)
			}
		})
	}
	model.kind, model.step = startWizard, wizardMessage
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.step != wizardStart {
		t.Fatal("timer wizard should skip the end step when going back")
	}
	model.step = wizardProject
	model, command := wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEsc})
	if !model.canceled || command == nil {
		t.Fatal("escape from project did not cancel")
	}
	model.canceled = false
	model, command = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyCtrlC})
	if !model.canceled || command == nil {
		t.Fatal("Ctrl+C did not cancel")
	}
	model.step = wizardConfirm
	model, command = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if !model.confirmed || command == nil {
		t.Fatal("confirmation y did not finish")
	}
}

func TestSessionWizardValidationAndViews(t *testing.T) {
	s := wizardTestService(t)
	model := newSessionWizardModel(s, addWizard, sessionWizardDefaults{Project: "alpha"})
	model.width = 0
	for _, step := range []sessionWizardStep{wizardProject, wizardTags, wizardStart, wizardEnd, wizardMessage, wizardConfirm} {
		model.step = step
		model.errorText = "validation failed"
		if view := model.View(); !strings.Contains(view, "ADD A COMPLETED SESSION") {
			t.Fatalf("step %v missing title: %s", step, view)
		}
	}
	model.step = wizardStart
	model.input = nil
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.errorText != "Enter a start time." {
		t.Fatalf("missing start validation=%q", model.errorText)
	}
	model.startTime = s.Now().Add(-time.Hour)
	model.step = wizardEnd
	for _, input := range []string{"", "bad time", "10:00"} {
		model.input = []rune(input)
		model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
		if model.errorText == "" || model.step != wizardEnd {
			t.Fatalf("invalid end %q advanced: %#v", input, model)
		}
	}
	model.kind, model.step = startWizard, wizardStart
	model.input = []rune("2026-10-29 09:00")
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(model.errorText, "future") || model.step != wizardStart {
		t.Fatalf("future start was accepted: %#v", model)
	}
	model.input = []rune("15:30")
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.step != wizardMessage || !model.hasStart {
		t.Fatalf("valid explicit timer start rejected: %#v", model)
	}
	model.defaultTags = []string{"default", "default"}
	model.tags = []string{"default", "ticket"}
	model.step, model.messageText, model.errorText = wizardConfirm, "my note", ""
	if view := model.View(); !strings.Contains(view, "START A SESSION") || !strings.Contains(view, "my note") {
		t.Fatalf("confirmation missing details: %s", view)
	}
	if got := strings.Join(model.effectiveTags(), ","); got != "default,ticket" {
		t.Fatalf("timer tags=%q", got)
	}
	model.kind = addWizard
	if got := len(model.effectiveTags()); got != 4 {
		t.Fatalf("add wizard should preserve additive defaults, got %d tags", got)
	}
	model.step, model.projectTags = wizardTags, []string{"ticket", "available"}
	model.input = []rune("avail")
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyUp})
	if view := model.View(); !strings.Contains(view, "Selected:") || !strings.Contains(view, "available") {
		t.Fatalf("tag view missing selected and available tags: %s", view)
	}
	model.projectTags, model.input = nil, nil
	model.acceptSuggestedTag()
	model.step = wizardMessage
	if !strings.Contains(model.View(), "Optional message") {
		t.Fatal("message placeholder missing")
	}
	model.step = wizardStart
	if !strings.Contains(model.View(), "Enter date and time") {
		t.Fatal("required time placeholder missing")
	}
	model.kind = startWizard
	if !strings.Contains(model.View(), "Now") {
		t.Fatal("timer default time missing")
	}
	model.step = wizardConfirm
	model.startText = ""
	if !strings.Contains(model.View(), "(Now)") {
		t.Fatal("default start confirmation missing Now")
	}
}

func TestSessionWizardRecentActiveSuggestionsAndPrecision(t *testing.T) {
	s := wizardTestService(t)
	s.State = store.State{Project: "active", Start: s.Now().Unix(), Tags: []string{"A", "", "A"}}
	s.Concurrent = []store.ActiveTimer{{Project: "active", Tags: []string{"B"}}, {Project: "other", Tags: []string{"C"}}}
	s.Frames = []store.Frame{{Project: "active", Tags: []string{"A", "D", ""}}, {Project: "old"}, {Project: "old"}}
	if got := strings.Join(recentProjects(s), ","); got != "active,other,old" {
		t.Fatalf("active projects not first or not deduplicated: %s", got)
	}
	if got := strings.Join(recentTagsForProject(s.Frames, s.RunningTimers(), "active"), ","); got != "A,B,D" {
		t.Fatalf("active/recent tag order=%s", got)
	}
	if err := s.Config.Set("options", "minute_precision", "false"); err != nil {
		t.Fatal(err)
	}
	now := s.Now().Add(37 * time.Second)
	s.Now = func() time.Time { return now }
	model := newSessionWizardModel(s, startWizard, sessionWizardDefaults{})
	if !model.currentTime().Equal(now) {
		t.Fatal("disabled minute precision discarded seconds")
	}
}

func TestSessionWizardDisplaysSearchChoicesAndConfiguredMinutePrecision(t *testing.T) {
	s := wizardTestService(t)
	if err := s.Config.Set("options", "minute_precision", "true"); err != nil {
		t.Fatal(err)
	}
	model := newSessionWizardModel(s, startWizard, sessionWizardDefaults{Project: "alpha"})
	model.projects = []string{"alpha", "beta"}
	model.input = nil
	if view := model.View(); !strings.Contains(view, "Type to search") || !strings.Contains(view, "alpha") || !strings.Contains(view, "beta") {
		t.Fatalf("project choices missing: %s", view)
	}
	model.input = []rune("alpha")
	model, _ = wizardUpdate(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.project != "alpha" || model.step != wizardTags {
		t.Fatal("unchanged default project not accepted")
	}
	model.input = nil
	if !strings.Contains(model.View(), "Type tags") {
		t.Fatal("tag prompt missing")
	}
	model.step = wizardProject
	if model.previousStep() != wizardProject {
		t.Fatal("first wizard step moved backwards")
	}
}
