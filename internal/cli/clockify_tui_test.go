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
}
