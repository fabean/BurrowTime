package cli

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fabean/BurrowTime/internal/config"
	"github.com/fabean/BurrowTime/internal/store"
	"github.com/fabean/BurrowTime/internal/watson"
)

func TestSplitCommandLine(t *testing.T) {
	want := []string{"--from", "2026-08-17 09:00", "multi word", "+tag"}
	got := splitCommandLine(`--from "2026-08-17 09:00" 'multi word' +tag`)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %#v, got %#v", want, got)
	}
}

func TestDashboardNavigatesToNativeLogAndReportViews(t *testing.T) {
	model := tuiTestModel()

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	model = updated.(dashboardModel)
	if model.screen != tuiLog {
		t.Fatalf("screen=%v, want log", model.screen)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(dashboardModel)
	if model.screen != tuiReport {
		t.Fatalf("screen=%v, want report", model.screen)
	}
}

func TestDashboardFilterMatchesProjectsTagsAndIDs(t *testing.T) {
	model := tuiTestModel()
	model.rangeIndex = 4

	model.filter = "client"
	if got := model.tuiFrames(false); len(got) != 1 || got[0].Project != "alpha" {
		t.Fatalf("tag filter returned %#v", got)
	}
	model.filter = "bbbbbbb"
	if got := model.tuiFrames(false); len(got) != 1 || got[0].Project != "beta" {
		t.Fatalf("ID filter returned %#v", got)
	}
	model.filter = "monday"
	if got := model.tuiFrames(false); len(got) != 2 {
		t.Fatalf("date filter returned %d frames, want 2", len(got))
	}
}

func TestDashboardCommandLauncherReturnsExistingCLIArguments(t *testing.T) {
	model := tuiTestModel()
	model.cursor = 2
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(dashboardModel)
	if model.inputMode != tuiInputCommand || model.command.name != "start" {
		t.Fatalf("did not open start command: %#v", model)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("deep work +focus")})
	model = updated.(dashboardModel)
	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(dashboardModel)
	if !reflect.DeepEqual(model.choice, []string{"start", "deep", "work", "+focus"}) {
		t.Fatalf("choice=%#v", model.choice)
	}
	if command == nil {
		t.Fatal("entering a command should quit the TUI and hand off to Cobra")
	}
}

func TestDashboardHomeFitsAnEightyColumnTerminal(t *testing.T) {
	model := tuiTestModel()
	model.width, model.height = 80, 24
	view := model.View()
	if !strings.Contains(view, "BURROWTIME") || !strings.Contains(view, "QUICK ACTIONS") || !strings.Contains(view, "RECENT") {
		t.Fatalf("home view is missing dashboard sections: %q", view)
	}
	for lineNumber, line := range strings.Split(view, "\n") {
		if width := lipgloss.Width(line); width > 80 {
			t.Fatalf("line %d is %d columns wide: %q", lineNumber+1, width, line)
		}
	}
}

func TestDashboardShowsAllRunningTimers(t *testing.T) {
	model := tuiTestModel()
	model.service.State = store.State{Project: "primary", Start: model.now.Add(-time.Hour).Unix()}
	model.service.Concurrent = []store.ActiveTimer{{ID: "abcdef012345", Project: "secondary", Start: model.now.Add(-30 * time.Minute).Unix()}}
	view := model.View()
	for _, want := range []string{"2 TIMERS RUNNING", "primary", "secondary"} {
		if !strings.Contains(view, want) {
			t.Fatalf("dashboard does not contain %q: %q", want, view)
		}
	}
}

func tuiTestModel() dashboardModel {
	now := time.Date(2026, time.August, 17, 15, 0, 0, 0, time.UTC)
	alphaStop := now.Add(-3 * time.Hour).Unix()
	betaStop := now.Add(-time.Hour).Unix()
	return dashboardModel{
		service: &watson.Service{
			Config: config.New(),
			Now:    func() time.Time { return now },
			Frames: []store.Frame{
				{Start: now.Add(-4 * time.Hour).Unix(), Stop: &alphaStop, Project: "alpha", ID: "aaaaaaaaaaaaaaaa", Tags: []string{"client"}},
				{Start: now.Add(-2 * time.Hour).Unix(), Stop: &betaStop, Project: "beta", ID: "bbbbbbbbbbbbbbbb", Tags: []string{"internal"}},
			},
		},
		screen:        tuiHome,
		rangeIndex:    1,
		width:         90,
		height:        28,
		now:           now,
		cursorVisible: true,
	}
}

func TestProjectAndMultiwordTags(t *testing.T) {
	project, tags := parseProjectTags([]string{"client", "work", "+code", "review", "+urgent"})
	if project != "client work" || !reflect.DeepEqual(tags, []string{"code review", "urgent"}) {
		t.Fatalf("project=%q tags=%#v", project, tags)
	}
}

func TestWatsonDateFormats(t *testing.T) {
	now := time.Date(2026, 8, 17, 16, 0, 0, 0, time.UTC)
	cases := map[string]string{"2018": "2018-01-01 00:00:00", "2018-04": "2018-04-01 00:00:00", "2018/4/10": "2018-04-10 00:00:00", "20180410": "2018-04-10 00:00:00", "2018-123": "2018-05-03 00:00:00", "2018-W08-2": "2018-02-20 00:00:00", "2018W08": "2018-02-19 00:00:00", "14:05": "2026-08-17 14:05:00", "2018-04-10 12": "2018-04-10 12:00:00", "2018-04-10 12:30:43+03:00": "2018-04-10 12:30:43"}
	for input, want := range cases {
		got, err := parseTime(input, now)
		if err != nil {
			t.Errorf("%s: %v", input, err)
			continue
		}
		if formatted := got.Format("2006-01-02 15:04:05"); formatted != want {
			t.Errorf("%s: want %s got %s", input, want, formatted)
		}
	}
}

func TestNonexistentDSTWallTimeMatchesDateutil(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	got := localWallTime(2024, time.March, 10, 2, 30, 0, 0, loc)
	if got.Unix() != 1710052200 || got.Format("15:04 MST") != "02:30 EDT" {
		t.Fatalf("unexpected nonexistent wall time: %s (%d)", got, got.Unix())
	}
}

func TestPythonStrftimeDirectives(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	value := time.Unix(1730611800, 0).In(loc)
	format := "%a|%A|%b|%B|%y|%Y|%C|%m|%d|%e|%H|%I|%M|%S|%f|%p|%P|%z|%Z|%j|%U|%W|%w|%u|%V|%G|%g|%c|%x|%X|%%|%-d|%F|%R|%T|%%Y"
	want := "Sun|Sunday|Nov|November|24|2024|20|11|03| 3|01|01|30|00|000000|AM|am|-0400|EDT|308|44|44|0|7|44|2024|24|Sun Nov  3 01:30:00 2024|11/03/24|01:30:00|%|3|2024-11-03|01:30|01:30:00|%Y"
	if got := pythonStrftime(value, format); got != want {
		t.Fatalf("want %q\n got %q", want, got)
	}
}

func TestNegativeFrameIndexNormalization(t *testing.T) {
	want := []string{"--watson-dir", "x", "remove", "--force", "--", "-2"}
	got := normalizeNegativeFrameArgs([]string{"--watson-dir", "x", "remove", "-2", "--force"})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %#v got %#v", want, got)
	}
}
