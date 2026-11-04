package cli

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fabean/BurrowTime/internal/integrations"
	"github.com/fabean/BurrowTime/internal/store"
)

func clockifyKey(m clockifyModel, key string) clockifyModel {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	switch key {
	case "enter":
		msg.Type = tea.KeyEnter
	case "esc":
		msg.Type = tea.KeyEsc
	case "down":
		msg.Type = tea.KeyDown
	case "up":
		msg.Type = tea.KeyUp
	case "ctrl+u":
		msg.Type = tea.KeyCtrlU
	case "ctrl+c":
		msg.Type = tea.KeyCtrlC
	}
	next, _ := m.Update(msg)
	return next.(clockifyModel)
}
func reviewModel() clockifyModel {
	m := newClockifyModel([]integrations.Project{{ID: "p", Name: "Client portal"}, {ID: "leica", Name: "BU2: Leica"}})
	m.screen = clockifyReview
	m.queue = []integrations.Receipt{{FrameID: "f1", RecordedSeconds: 960, ExportSeconds: 1800, Entry: integrations.Entry{Start: "2026-09-09T10:00:00Z", End: "2026-09-09T10:30:00Z", ProjectID: "p", Description: "portal +PORTAL-42 [burrowtime:f1]"}}}
	return m
}

func TestClockifyTUISuggestionsAndLiveSearch(t *testing.T) {
	m := newClockifyModel([]integrations.Project{{ID: "other", Name: "Other"}, {ID: "p", Name: "BU2: Leica"}})
	m.local = "leica"
	if m.filtered()[0].ID != "p" || !strings.Contains(m.View(), "suggested") {
		t.Fatal("missing suggested match")
	}
	m = clockifyKey(m, "/")
	m = clockifyKey(m, "other")
	if len(m.filtered()) != 1 || m.filtered()[0].ID != "other" {
		t.Fatal("live search failed")
	}
	m = clockifyKey(m, "enter")
	if m.done {
		t.Fatal("search Enter should return to browsing")
	}
	m = clockifyKey(m, "enter")
	if !m.done || m.selected != "other" {
		t.Fatal("selection failed")
	}
}

func TestClockifyTUIShowsAndSearchesClients(t *testing.T) {
	m := newClockifyModel([]integrations.Project{{ID: "a", Name: "Development - US", ClientName: "Winebow"}, {ID: "b", Name: "Development - US", ClientName: "Leica"}, {ID: "c", Name: "Internal"}})
	m.height = 40
	view := m.View()
	for _, label := range []string{"Client: Winebow", "Client: Leica", "Client: No client"} {
		if !strings.Contains(view, label) {
			t.Fatalf("missing %q", label)
		}
	}
	m.query = "winebow"
	if matches := m.filtered(); len(matches) != 1 || matches[0].ID != "a" {
		t.Fatal("cannot search by client")
	}
	if m.projectName("a") != "Winebow / Development - US" {
		t.Fatal("review loses client context")
	}
}

func TestClockifyTUIReviewEditsAndExplicitConfirmation(t *testing.T) {
	m := reviewModel()
	m = clockifyKey(m, "enter")
	m = clockifyKey(m, "t")
	m = clockifyKey(m, "ctrl+u")
	m = clockifyKey(m, "bad")
	m = clockifyKey(m, "enter")
	if m.message == "" || m.field != "duration" {
		t.Fatal("invalid duration silently accepted")
	}
	m = clockifyKey(m, "ctrl+u")
	m = clockifyKey(m, "45m")
	m = clockifyKey(m, "enter")
	if m.queue[0].ExportSeconds != 2700 || m.queue[0].Entry.End != "2026-09-09T10:45:00Z" {
		t.Fatal("duration edit failed")
	}
	m = clockifyKey(m, "d")
	m = clockifyKey(m, "ctrl+u")
	m = clockifyKey(m, "PORTAL-42 reviewed")
	m = clockifyKey(m, "enter")
	if m.queue[0].Entry.Description != "PORTAL-42 reviewed" {
		t.Fatal("export description includes unwanted metadata")
	}
	m = clockifyKey(m, "b")
	m = clockifyKey(m, "p")
	m = clockifyKey(m, "down")
	m = clockifyKey(m, "enter")
	if m.screen != clockifyEditor || m.queue[0].Entry.ProjectID != "leica" || !m.queue[0].Entry.Billable {
		t.Fatal("project/billable edit failed")
	}
	m = clockifyKey(m, "esc")
	m = clockifyKey(m, "p")
	if m.screen != clockifyConfirm || m.done {
		t.Fatal("upload bypassed confirmation")
	}
	m = clockifyKey(m, "enter")
	if m.done {
		t.Fatal("Enter accidentally confirmed upload")
	}
	m = clockifyKey(m, "y")
	if !m.done {
		t.Fatal("explicit upload not confirmed")
	}
}

func TestClockifyTUICancelSkipAndDryRun(t *testing.T) {
	for _, key := range []string{"q", "esc", "ctrl+c"} {
		m := clockifyKey(reviewModel(), key)
		if m.done {
			t.Fatalf("%s approved upload", key)
		}
	}
	m := newClockifyModel(nil)
	m = clockifyKey(m, "enter")
	if m.done {
		t.Fatal("empty picker selected entry")
	}
	m = clockifyKey(m, "s")
	if !m.done || m.selected != "" {
		t.Fatal("skip failed")
	}
	m = reviewModel()
	m = clockifyKey(m, "s")
	if len(m.queue) != 0 {
		t.Fatal("skip entry failed")
	}
	m = reviewModel()
	m.dryRun = true
	m = clockifyKey(m, "p")
	if !m.done || m.screen == clockifyConfirm {
		t.Fatal("dry run reached upload confirmation")
	}
}

func TestClockifyTUIFitsTerminal(t *testing.T) {
	for _, size := range [][2]int{{40, 24}, {80, 24}, {100, 32}, {120, 40}} {
		for _, screen := range []clockifyScreen{clockifyPicker, clockifyReview, clockifyEditor, clockifyConfirm} {
			m := reviewModel()
			m.width, m.height = size[0], size[1]
			m.screen = screen
			m.local = "leica"
			for i := 0; i < 40; i++ {
				m.projects = append(m.projects, integrations.Project{ID: fmt.Sprint(i), Name: strings.Repeat("遠", 40)})
			}
			view := m.View()
			if lipgloss.Width(view) > m.width {
				t.Errorf("screen %d width %d exceeds %d", screen, lipgloss.Width(view), m.width)
			}
			if lipgloss.Height(view) > m.height {
				t.Errorf("screen %d height %d exceeds %d", screen, lipgloss.Height(view), m.height)
			}
		}
	}
}

func TestClockifyMapperOnlySelectsUnmappedInRange(t *testing.T) {
	stop := int64(2000)
	frames := []store.Frame{{Start: 1000, Stop: &stop, Project: "portal"}, {Start: 1000, Stop: &stop, Project: "mapped"}, {Start: 100, Stop: &stop, Project: "outside"}, {Start: 1000, Project: "running"}}
	zeroStop := int64(1000)
	frames = append(frames, store.Frame{Start: zeroStop, Stop: &zeroStop, Project: "empty"})
	c := integrations.Config{Projects: map[string]integrations.Mapping{"mapped": {Connection: "other", ProjectID: "other"}}}
	opts := integrations.Options{Connection: "work", From: time.Unix(900, 0), To: time.Unix(1100, 0)}
	calls := 0
	p := exportPrompter{projects: []integrations.Project{{ID: "p", Name: "portal"}}, run: func(m clockifyModel) (clockifyModel, error) {
		calls++
		if m.local != "portal" {
			t.Fatal("prompted for excluded frame")
		}
		return clockifyKey(m, "enter"), nil
	}}
	if err := p.mapMissing(&c, frames, &opts); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || c.Projects["portal"].ProjectID != "p" {
		t.Fatal("mapping failed")
	}
	if !c.Projects["portal"].Billable {
		t.Fatal("new Clockify mapping must default to billable")
	}
	if c.Projects["mapped"].Billable {
		t.Fatal("changed existing mapping's billable setting")
	}
}

func TestClockifyNavigationEditingAndEmptyViews(t *testing.T) {
	t.Parallel()
	m := newClockifyModel([]integrations.Project{{ID: "a", Name: "Alpha"}, {ID: "b", Name: "Beta"}})
	if m.Init() != nil {
		t.Fatal("unexpected initial command")
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = next.(clockifyModel)
	if m.width != 100 || m.height != 40 {
		t.Fatal("resize ignored")
	}
	m = clockifyKey(m, "up")
	if m.cursor != 1 {
		t.Fatal("up did not wrap")
	}
	m = clockifyKey(m, "down")
	if m.cursor != 0 {
		t.Fatal("down did not wrap")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = next.(clockifyModel)
	if m.cursor != 1 {
		t.Fatal("page down ignored")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = next.(clockifyModel)
	if m.cursor != 0 {
		t.Fatal("page up ignored")
	}
	m = clockifyKey(m, "/")
	m = clockifyKey(m, "é")
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(clockifyModel)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(clockifyModel)
	if m.query != "é" {
		t.Fatalf("unicode editing %q", m.query)
	}
	m = clockifyKey(m, "esc")
	if m.searching {
		t.Fatal("did not stop searching")
	}
	m = clockifyKey(m, "esc")
	if m.query != "" {
		t.Fatal("did not clear query")
	}
	m = reviewModel()
	m.screen = clockifyEditor
	m = clockifyKey(m, "p")
	m = clockifyKey(m, "esc")
	if m.screen != clockifyEditor || m.pickingForEntry {
		t.Fatal("picker cancel failed")
	}
	m = clockifyKey(m, "p")
	m = clockifyKey(m, "s")
	if m.screen != clockifyEditor || m.pickingForEntry {
		t.Fatal("picker skip failed")
	}
	m = clockifyKey(m, "d")
	m = clockifyKey(m, "esc")
	if m.field != "" {
		t.Fatal("did not cancel description edit")
	}
	m = clockifyKey(m, "d")
	m.input = " "
	m = clockifyKey(m, "enter")
	if m.message == "" {
		t.Fatal("accepted blank description")
	}
	if !strings.Contains(m.View(), "description") {
		t.Fatal("missing validation view")
	}
	m.input = strings.Repeat("a", 3000)
	m = clockifyKey(m, "b")
	if len([]rune(m.input)) != 3000 {
		t.Fatal("input exceeded bound")
	}
	m.input = "reviewed"
	m = clockifyKey(m, "enter")
	if m.queue[0].Entry.Description != "reviewed" {
		t.Fatal("did not save description")
	}
	m = clockifyKey(m, "t")
	m.input = "1h"
	m.queue[0].Entry.Start = "bad"
	m = clockifyKey(m, "enter")
	if !strings.Contains(m.message, "Invalid entry start") {
		t.Fatal("accepted invalid start")
	}
	m = clockifyKey(m, "esc")
	m = clockifyKey(m, "enter")
	m = clockifyKey(m, "up")
	m = clockifyKey(m, "down")
	m = clockifyKey(m, "p")
	m = clockifyKey(m, "n")
	if m.screen != clockifyReview || m.done {
		t.Fatal("decline failed")
	}
	m = reviewModel()
	m.provider = "Timetable"
	m.screen = clockifyEditor
	before := m.queue[0].Entry.Billable
	m = clockifyKey(m, "b")
	if m.queue[0].Entry.Billable != before || strings.Contains(m.View(), "Billable") {
		t.Fatal("Timetable exposes unsupported billable edit")
	}
	if got := m.projectName("missing"); got != "missing" {
		t.Fatalf("unknown project %q", got)
	}
	m = reviewModel()
	m.queue = nil
	if !strings.Contains(m.View(), "Nothing will be uploaded") {
		t.Fatal("missing empty batch view")
	}
	m = clockifyKey(m, "enter")
	if m.screen != clockifyReview {
		t.Fatal("edited empty queue")
	}
	m = clockifyKey(m, "p")
	if !m.done {
		t.Fatal("empty review did not finish")
	}
	m = newClockifyModel(nil)
	if !strings.Contains(m.View(), "No matching projects") {
		t.Fatal("missing empty picker")
	}
	m.width = 20
	if !strings.Contains(m.View(), "Enlarge terminal") {
		t.Fatal("missing small terminal notice")
	}
}

func TestClockifyPrompterMapsTimetableSkipsAndPropagatesErrors(t *testing.T) {
	t.Parallel()
	stop := int64(2000)
	frames := []store.Frame{{Start: 1000, Stop: &stop, Project: "portal"}}
	for _, tc := range []struct {
		name          string
		skip, failure bool
	}{{name: "map"}, {name: "skip", skip: true}, {name: "failed", failure: true}} {
		t.Run(tc.name, func(t *testing.T) {
			c := integrations.Config{Projects: map[string]integrations.Mapping{}}
			opts := integrations.Options{Connection: "timetable"}
			var out strings.Builder
			p := exportPrompter{out: &out, provider: "Timetable", run: func(m clockifyModel) (clockifyModel, error) {
				if m.provider != "Timetable" {
					t.Fatal("lost provider")
				}
				if tc.failure {
					return m, fmt.Errorf("canceled")
				}
				m.done = true
				if !tc.skip {
					m.selected = "project"
				}
				return m, nil
			}}
			err := p.mapMissing(&c, frames, &opts)
			if (err != nil) != tc.failure {
				t.Fatalf("got %v", err)
			}
			if tc.failure {
				return
			}
			if tc.skip {
				if !opts.SkipProjects["portal"] || !strings.Contains(out.String(), "Skipping") {
					t.Fatal("did not record skip")
				}
			} else if c.Routes["timetable"]["portal"].ProjectID != "project" {
				t.Fatal("did not save route")
			}
		})
	}
	for _, tc := range []struct {
		name    string
		failure bool
	}{{name: "review"}, {name: "canceled", failure: true}} {
		t.Run(tc.name, func(t *testing.T) {
			source := reviewModel().queue
			p := exportPrompter{provider: "Timetable", dryRun: true, run: func(m clockifyModel) (clockifyModel, error) {
				if m.provider != "Timetable" || !m.dryRun || m.screen != clockifyReview {
					t.Fatal("lost review context")
				}
				if tc.failure {
					return m, fmt.Errorf("canceled")
				}
				m.queue[0].Entry.Description = "edited"
				return m, nil
			}}
			got, err := p.review(source)
			if (err != nil) != tc.failure {
				t.Fatalf("got %v", err)
			}
			if !tc.failure && (got[0].Entry.Description != "edited" || source[0].Entry.Description == "edited") {
				t.Fatal("review did not isolate export edits")
			}
		})
	}
	if projectScore("", "anything") != 0 || projectScore("", "") != 10000 {
		t.Fatal("empty score")
	}
	if projectScore(strings.Repeat("a", 250), strings.Repeat("b", 250)) < 0 {
		t.Fatal("negative score")
	}
}

func TestClockifyViewsShowPreviewSearchAndValidation(t *testing.T) {
	t.Parallel()
	m := reviewModel()
	m.dryRun = true
	if !strings.Contains(m.View(), "DRY RUN") {
		t.Fatal("preview label missing")
	}
	m.screen = clockifyPicker
	m.searching = true
	m.query = "no match"
	view := m.View()
	if !strings.Contains(view, "no match") || !strings.Contains(view, "No matching projects") {
		t.Fatal("search state missing")
	}
	m = reviewModel()
	m.screen = clockifyEditor
	m.field = "duration"
	m.input = "bad"
	m.message = "Use a positive duration"
	if !strings.Contains(m.View(), m.message) {
		t.Fatal("validation missing")
	}
	m = newClockifyModel(nil)
	m = clockifyKey(m, "esc")
	if m.done {
		t.Fatal("empty picker approved export")
	}
	if got := clockifySafe("line\n\t\x1b[31m"); got != "line   [31m" {
		t.Fatalf("unsafe controls %q", got)
	}
}
