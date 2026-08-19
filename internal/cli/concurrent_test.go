package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fabean/BurrowTime/internal/store"
	"github.com/fabean/BurrowTime/internal/watson"
)

func TestConcurrentCLIRequiresExplicitStopSelection(t *testing.T) {
	dir := t.TempDir()
	if output, err := runBurrowTimeCommand(dir, "start", "one", "+first"); err != nil {
		t.Fatalf("start: %v\n%s", err, output)
	}
	if _, err := runBurrowTimeCommand(dir, "start", "two"); err == nil || !strings.Contains(err.Error(), "already started") {
		t.Fatalf("ordinary start should remain safe, got %v", err)
	}
	if output, err := runBurrowTimeCommand(dir, "start", "--concurrent", "two", "+second"); err != nil {
		t.Fatalf("concurrent start: %v\n%s", err, output)
	}
	status, err := runBurrowTimeCommand(dir, "status")
	if err != nil || !strings.Contains(status, "2 timers running") || !strings.Contains(status, "one") || !strings.Contains(status, "two") {
		t.Fatalf("status=%q err=%v", status, err)
	}
	if _, err := runBurrowTimeCommand(dir, "stop"); err == nil || !strings.Contains(err.Error(), "--timer <id> or --all") {
		t.Fatalf("non-interactive ambiguous stop should fail, got %v", err)
	}

	s, err := watson.OpenData(dir)
	if err != nil {
		t.Fatal(err)
	}
	ref := s.Concurrent[0].ID[:7]
	if output, err := runBurrowTimeCommand(dir, "stop", "--timer", ref); err != nil || !strings.Contains(output, "Stopping project two") {
		t.Fatalf("targeted stop output=%q err=%v", output, err)
	}
	if output, err := runBurrowTimeCommand(dir, "stop", "--all"); err != nil || !strings.Contains(output, "Stopping project one") {
		t.Fatalf("stop all output=%q err=%v", output, err)
	}
}

func TestStartStopFlagReplacesAllRunningTimers(t *testing.T) {
	dir := t.TempDir()
	if _, err := runBurrowTimeCommand(dir, "start", "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := runBurrowTimeCommand(dir, "start", "--concurrent", "two"); err != nil {
		t.Fatal(err)
	}
	output, err := runBurrowTimeCommand(dir, "start", "--stop", "replacement")
	if err != nil {
		t.Fatalf("replace: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Stopped 2 running timers") || !strings.Contains(output, "Starting project replacement") {
		t.Fatalf("replace output: %q", output)
	}
	s, err := watson.OpenData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Frames) != 2 || s.State.Project != "replacement" || len(s.Concurrent) != 0 {
		t.Fatalf("replacement state: frames=%d state=%#v concurrent=%#v", len(s.Frames), s.State, s.Concurrent)
	}
}

func TestStopPickerCanChooseTimerOrAll(t *testing.T) {
	timers := []store.ActiveTimer{{ID: "primary", Project: "one", Primary: true}, {ID: "abcdef", Project: "two"}}
	model := stopPickerModel{timers: timers, width: 80}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated, command := updated.(stopPickerModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	selected := updated.(stopPickerModel)
	if selected.selection != "abcdef" || command == nil {
		t.Fatalf("selection=%q command=%v", selected.selection, command)
	}
	model = stopPickerModel{timers: timers, width: 80}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if updated.(stopPickerModel).selection != stopAllSelection {
		t.Fatalf("all selection=%q", updated.(stopPickerModel).selection)
	}
}

func TestStartConflictPickerCanReplaceOrStartConcurrently(t *testing.T) {
	timers := []store.ActiveTimer{
		{ID: "primary", Project: "one", Tags: []string{"first"}, Start: 100, Primary: true},
		{ID: "abcdef", Project: "two", Tags: []string{"second"}, Start: 200},
	}

	model := startConflictModel{timers: timers, project: "replacement", tags: []string{"new"}, now: time.Unix(300, 0), width: 80}
	if view := model.View(); !strings.Contains(view, "TIMER ALREADY RUNNING") || !strings.Contains(view, "one") || !strings.Contains(view, "#first") || !strings.Contains(view, "replacement") || !strings.Contains(view, "#new") {
		t.Fatalf("picker view is missing timer context:\n%s", view)
	}
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if selected := updated.(startConflictModel); selected.selection != startConflictReplace || command == nil {
		t.Fatalf("replace selection=%q command=%v", selected.selection, command)
	}

	model = startConflictModel{timers: timers, project: "replacement", width: 80}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated, command = updated.(startConflictModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if selected := updated.(startConflictModel); selected.selection != startConflictConcurrent || command == nil {
		t.Fatalf("concurrent selection=%q command=%v", selected.selection, command)
	}

	model = startConflictModel{timers: timers, project: "replacement", width: 80}
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if canceled := updated.(startConflictModel); !canceled.canceled || canceled.selection != "" || command == nil {
		t.Fatalf("canceled=%v selection=%q command=%v", canceled.canceled, canceled.selection, command)
	}
}

func runBurrowTimeCommand(dir string, args ...string) (string, error) {
	var output bytes.Buffer
	a := &app{name: "burrowtime"}
	root := a.root()
	root.SetArgs(append([]string{"--watson-dir", dir}, args...))
	root.SetIn(strings.NewReader(""))
	root.SetOut(&output)
	root.SetErr(&output)
	err := root.Execute()
	return output.String(), err
}
