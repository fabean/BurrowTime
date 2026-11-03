package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fabean/BurrowTime/internal/report"
	"github.com/fabean/BurrowTime/internal/store"
	"github.com/fabean/BurrowTime/internal/watson"
)

func TestConcurrentStartJSONReturnsExactTimerID(t *testing.T) {
	dir := t.TempDir()
	firstOutput, err := runBurrowTimeCommand(dir, "start", "--concurrent", "--json", "one", "+first")
	if err != nil {
		t.Fatal(err)
	}
	var first store.ActiveTimer
	if err := json.Unmarshal([]byte(firstOutput), &first); err != nil {
		t.Fatalf("decode first timer: %v\n%s", err, firstOutput)
	}
	if first.ID != watson.PrimaryTimerID || first.Project != "one" || !reflect.DeepEqual(first.Tags, []string{"first"}) {
		t.Fatalf("first timer: %#v", first)
	}

	secondOutput, err := runBurrowTimeCommand(dir, "start", "--concurrent", "--json", "two", "+second")
	if err != nil {
		t.Fatal(err)
	}
	var second store.ActiveTimer
	if err := json.Unmarshal([]byte(secondOutput), &second); err != nil {
		t.Fatalf("decode second timer: %v\n%s", err, secondOutput)
	}
	if len(second.ID) != 32 || second.Project != "two" || !reflect.DeepEqual(second.Tags, []string{"second"}) {
		t.Fatalf("second timer: %#v", second)
	}
	if _, err := runBurrowTimeCommand(dir, "stop", "--timer", second.ID); err != nil {
		t.Fatalf("stop exact timer: %v", err)
	}
	status, err := runBurrowTimeCommand(dir, "status")
	if err != nil || !strings.Contains(status, "Project one") || strings.Contains(status, "Project two") {
		t.Fatalf("unexpected status after targeted stop: %q, %v", status, err)
	}
}

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

func TestMinutePrecisionFloorsOrdinaryStartAndStopWithoutPreviousFrame(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("[options]\ntime_precision = minute\n"), 0600); err != nil {
		t.Fatal(err)
	}
	output, err := runBurrowTimeCommand(dir, "start", "project")
	if err != nil {
		t.Fatalf("start: %v\n%s", err, output)
	}
	s, err := watson.OpenData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.State.Start%60 != 0 {
		t.Fatalf("normal start did not floor to a minute: %s", time.Unix(s.State.Start, 0))
	}
	output, err = runBurrowTimeCommand(dir, "stop")
	if err != nil {
		t.Fatalf("stop: %v\n%s", err, output)
	}
	s, err = watson.OpenData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Frames) != 1 || s.State.Running() || *s.Frames[0].Stop%60 != 0 || *s.Frames[0].Stop < s.Frames[0].Start {
		t.Fatalf("normal stop was not safely floored: state=%#v frames=%#v", s.State, s.Frames)
	}
}

func TestMinutePrecisionPreservesExplicitSeconds(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("[options]\ntime_precision = minute\n"), 0600); err != nil {
		t.Fatal(err)
	}
	started := time.Now().Add(-45 * time.Minute).Truncate(time.Minute).Add(17 * time.Second)
	stopped := started.Add(5*time.Minute + 25*time.Second)
	if output, err := runBurrowTimeCommand(dir, "start", "--at", started.Format("2006-01-02 15:04:05"), "project"); err != nil {
		t.Fatalf("start: %v\n%s", err, output)
	}
	s, err := watson.OpenData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.State.Start != started.Unix() {
		t.Fatalf("explicit start seconds changed: got %d want %d", s.State.Start, started.Unix())
	}
	if output, err := runBurrowTimeCommand(dir, "stop", "--at", stopped.Format("2006-01-02 15:04:05")); err != nil {
		t.Fatalf("stop: %v\n%s", err, output)
	}
	s, err = watson.OpenData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Frames) != 1 || *s.Frames[0].Stop != stopped.Unix() {
		t.Fatalf("explicit stop seconds changed: frames=%#v want stop=%d", s.Frames, stopped.Unix())
	}
}

func TestMinutePrecisionFloorsHumanConcurrentTimerStarts(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("[options]\ntime_precision = minute\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := runBurrowTimeCommand(dir, "start", "first"); err != nil {
		t.Fatalf("first start: %v\n%s", err, output)
	}
	if output, err := runBurrowTimeCommand(dir, "start", "--concurrent", "second"); err != nil {
		t.Fatalf("concurrent start: %v\n%s", err, output)
	}
	s, err := watson.OpenData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.State.Start%60 != 0 || len(s.Concurrent) != 1 || s.Concurrent[0].Start%60 != 0 {
		t.Fatalf("human timer starts were not floored: state=%#v concurrent=%#v", s.State, s.Concurrent)
	}
}

func TestStartAlignsMinutePrecisionWithinPreviousBoundaryMinute(t *testing.T) {
	for _, offset := range []int64{1, 30, 59} {
		t.Run(fmt.Sprintf("%d_seconds", offset), func(t *testing.T) {
			dir := t.TempDir()
			repo, err := store.New(dir)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			requested := now.Add(-3 * time.Minute).Truncate(time.Minute)
			previousStop := requested.Add(time.Duration(offset) * time.Second)
			stop := previousStop.Unix()
			previous := store.Frame{Start: requested.Add(-time.Hour).Unix(), Stop: &stop, Project: "old", ID: "11111111111111111111111111111111", Tags: []string{"ticket"}, UpdatedAt: stop}
			if err := repo.SaveFrames([]store.Frame{previous}); err != nil {
				t.Fatal(err)
			}
			input := requested.Format("3:04 PM")
			output, err := runBurrowTimeCommandInput(dir, "y\n", "start", "--at", input, "new")
			if err != nil {
				t.Fatalf("start: %v\n%s", err, output)
			}
			s, err := watson.OpenData(dir)
			if err != nil {
				t.Fatal(err)
			}
			if s.State.Start != previousStop.Unix() {
				t.Fatalf("new timer starts at %s; want aligned boundary %s", time.Unix(s.State.Start, 0), previousStop)
			}
			if len(s.Frames) != 1 || *s.Frames[0].Stop != stop {
				t.Fatalf("existing frame changed: %#v", s.Frames)
			}
			summary := report.Build(s.Frames, report.Options{})
			if summary.Time != report.Seconds(float64(3600+offset)) {
				t.Fatalf("report total changed after boundary alignment: %v", summary.Time)
			}
			if strings.Contains(output, "Align to") {
				t.Fatalf("same-minute alignment unexpectedly prompted: %q", output)
			}
		})
	}
}

func TestStartMinuteBoundaryExactMatchAndExistingOverlapRules(t *testing.T) {
	for _, tc := range []struct {
		name        string
		offset      int64
		inputSuffix string
		wantErr     bool
	}{
		{name: "exact boundary", offset: 0, inputSuffix: ""},
		{name: "meaningful overlap", offset: 90, inputSuffix: "", wantErr: true},
		{name: "explicit seconds", offset: 30, inputSuffix: ":00", wantErr: true},
		{name: "minute alignment needs no input", offset: 30, inputSuffix: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			repo, err := store.New(dir)
			if err != nil {
				t.Fatal(err)
			}
			requested := time.Now().Add(-3 * time.Minute).Truncate(time.Minute)
			previousStop := requested.Add(time.Duration(tc.offset) * time.Second)
			stop := previousStop.Unix()
			previous := store.Frame{Start: requested.Add(-time.Hour).Unix(), Stop: &stop, Project: "old", ID: "11111111111111111111111111111111", Tags: []string{}, UpdatedAt: stop}
			if err := repo.SaveFrames([]store.Frame{previous}); err != nil {
				t.Fatal(err)
			}
			input := requested.Format("3:04 PM")
			if tc.inputSuffix != "" {
				input = requested.Format("3:04:05 PM")
			}
			output, err := runBurrowTimeCommand(dir, "start", "--at", input, "new")
			if (err != nil) != tc.wantErr || strings.Contains(output, "Align to") {
				t.Fatalf("output=%q err=%v", output, err)
			}
			s, loadErr := watson.OpenData(dir)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if tc.wantErr && (s.State.Running() || len(s.Frames) != 1) {
				t.Fatalf("rejected start changed data: state=%#v frames=%#v", s.State, s.Frames)
			}
			if !tc.wantErr && (!s.State.Running() || s.State.Start != previousStop.Unix()) {
				t.Fatalf("exact boundary start state=%#v", s.State)
			}
		})
	}
}

func TestStopAlignsMinuteBoundaryWithoutPrompt(t *testing.T) {
	dir := t.TempDir()
	repo, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	requested := time.Now().Add(-3 * time.Minute).Truncate(time.Minute)
	start := requested.Add(30 * time.Second).Unix()
	if err := repo.SaveState(store.State{Project: "active", Start: start, Tags: []string{"ticket"}}); err != nil {
		t.Fatal(err)
	}
	output, err := runBurrowTimeCommand(dir, "stop", "--at", requested.Format("3:04 PM"))
	if err != nil || strings.Contains(output, "Align to") {
		t.Fatalf("output=%q err=%v", output, err)
	}
	s, err := watson.OpenData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.State.Running() || len(s.Frames) != 1 || s.Frames[0].Start != start || *s.Frames[0].Stop != start {
		t.Fatalf("aligned stop result: state=%#v frames=%#v", s.State, s.Frames)
	}
}

func TestStopBoundarySkipsAgentTimerAndTargetsSelectedConcurrentTimer(t *testing.T) {
	requested := time.Now().Add(-3 * time.Minute).Truncate(time.Minute)
	started := requested.Add(30 * time.Second)
	start := started.Unix()
	s := &watson.Service{
		Now:           time.Now,
		State:         store.State{Project: "agent", Start: start, Tags: []string{}},
		Concurrent:    []store.ActiveTimer{{ID: "concurrent-id", Project: "manual", Start: start, Primary: false}},
		AgentSessions: []store.AgentSession{{ID: "agent-session", TimerID: watson.PrimaryTimerID, Status: store.AgentSessionActive}},
	}
	requestedPtr := &requested
	unchanged := alignStopMinuteBoundary(s, watson.PrimaryTimerID, requestedPtr, true)
	if unchanged != requestedPtr {
		t.Fatalf("agent timer should not be normalized: time=%v", unchanged)
	}
	aligned := alignStopMinuteBoundary(s, "concurrent", requestedPtr, true)
	if aligned == nil || aligned.Unix() != start {
		t.Fatalf("selected concurrent timer not aligned: time=%v", aligned)
	}
	if s.State.Start != start || len(s.Concurrent) != 1 || s.Concurrent[0].Start != start {
		t.Fatalf("normalization changed timer state: %#v %#v", s.State, s.Concurrent)
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
	return runBurrowTimeCommandInput(dir, "", args...)
}

func runBurrowTimeCommandInput(dir, input string, args ...string) (string, error) {
	var output bytes.Buffer
	a := &app{name: "burrowtime"}
	root := a.root()
	root.SetArgs(append([]string{"--watson-dir", dir}, args...))
	root.SetIn(strings.NewReader(input))
	root.SetOut(&output)
	root.SetErr(&output)
	err := root.Execute()
	return output.String(), err
}
