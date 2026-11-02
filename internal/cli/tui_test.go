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

func TestDashboardCyclesBetweenLogReportAndAggregateViews(t *testing.T) {
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
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(dashboardModel)
	if model.screen != tuiAggregate {
		t.Fatalf("screen=%v, want aggregate", model.screen)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(dashboardModel)
	if model.screen != tuiLog {
		t.Fatalf("screen=%v, want log after cycling", model.screen)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model = updated.(dashboardModel)
	if model.screen != tuiAggregate {
		t.Fatalf("screen=%v, want aggregate via shortcut", model.screen)
	}
}

func TestDashboardQuickActionOrderAndSelection(t *testing.T) {
	model := tuiTestModel()
	view := model.homeView()
	logIndex, reportIndex, aggregateIndex := strings.Index(view, "Log"), strings.Index(view, "Report"), strings.Index(view, "Aggregate")
	if logIndex < 0 || reportIndex <= logIndex || aggregateIndex <= reportIndex {
		t.Fatalf("quick action order is wrong: %s", view)
	}
	model.cursor = 2
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := updated.(dashboardModel).screen; got != tuiAggregate {
		t.Fatalf("third quick action opened screen %v, want aggregate", got)
	}
	model.cursor = 3
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := updated.(dashboardModel); got.inputMode != tuiInputCommand || got.command.name != "start" {
		t.Fatalf("fourth quick action should still start: %#v", got)
	}
}

func TestAggregateViewGroupsDailyProjectAndTagTotals(t *testing.T) {
	model := tuiTestModel()
	model.now = time.Date(2026, time.August, 18, 13, 0, 0, 0, time.UTC)
	model.service.Now = func() time.Time { return model.now }
	model.rangeIndex = 4
	day := time.Date(2026, time.August, 17, 0, 0, 0, 0, time.UTC)
	makeFrame := func(start, stop time.Time, project string, tags ...string) store.Frame {
		end := stop.Unix()
		return store.Frame{Start: start.Unix(), Stop: &end, Project: project, Tags: tags}
	}
	model.service.Frames = []store.Frame{
		makeFrame(day.Add(9*time.Hour), day.Add(10*time.Hour), "RAF", "RAF-326"),
		makeFrame(day.Add(10*time.Hour), day.Add(11*time.Hour), "RAF", "RAF-326"),
		makeFrame(day.Add(10*time.Hour), day.Add(10*time.Hour+30*time.Minute), "Redesign", "FR-889"),
		makeFrame(day.Add(11*time.Hour), day.Add(12*time.Hour), "Uncategorized"),
		makeFrame(day.Add(23*time.Hour+30*time.Minute), day.Add(24*time.Hour+30*time.Minute), "RAF", "RAF-299"),
	}
	lines := strings.Join(model.aggregateLines(), "\n")
	for _, want := range []string{"Monday, August 17, 2026", "Tuesday, August 18, 2026", "RAF-326", "FR-889", "#RAF-299", "Uncategorized", "2h 00m 00s", "30m 00s"} {
		if !strings.Contains(lines, want) {
			t.Errorf("aggregate view missing %q:\n%s", want, lines)
		}
	}
	model.filter = "redesign"
	filtered := strings.Join(model.aggregateLines(), "\n")
	if !strings.Contains(filtered, "Redesign") || strings.Contains(filtered, "RAF") || strings.Contains(filtered, "Uncategorized") {
		t.Fatalf("filter was not applied to aggregate: %s", filtered)
	}
	model.filter = "not-a-project"
	if got := strings.Join(model.aggregateLines(), "\n"); !strings.Contains(got, "No tracked time matches") {
		t.Fatalf("missing empty state: %s", got)
	}
	model.filter = ""
	model.rangeIndex = 0
	today := strings.Join(model.aggregateLines(), "\n")
	if strings.Contains(today, "Monday, August 17, 2026") || !strings.Contains(today, "Tuesday, August 18, 2026") {
		t.Fatalf("today range did not filter aggregate days: %s", today)
	}
	model.width = 36
	for i, line := range model.aggregateLines() {
		if lipgloss.Width(line) > model.contentWidth() {
			t.Fatalf("aggregate line %d is too wide: %q", i, line)
		}
	}
}

func TestAggregateViewUsesSharedDailySummaryAndScrollBounds(t *testing.T) {
	model := tuiTestModel()
	model.rangeIndex = 4
	options, err := model.tuiOptions(true)
	if err != nil {
		t.Fatal(err)
	}
	want := aggregateDaily(model.tuiFrames(true), nil, model.now, options)
	got := model.aggregateSummaries()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TUI summaries differ from shared CLI daily aggregation")
	}
	model.screen = tuiAggregate
	model.height = 10
	model.scroll = model.maxScroll() + 20
	model.clampScroll()
	if model.scroll != model.maxScroll() {
		t.Fatalf("scroll=%d max=%d", model.scroll, model.maxScroll())
	}
	model.Update(tea.WindowSizeMsg{Width: 55, Height: 8})
	if model.scroll > model.maxScroll() {
		t.Fatalf("resize left scroll out of bounds: %d > %d", model.scroll, model.maxScroll())
	}
}

func TestAggregateViewRendersTitleRangeAndFooterControls(t *testing.T) {
	model := tuiTestModel()
	model.screen = tuiAggregate
	view := model.View()
	for _, want := range []string{"BURROWTIME", "Aggregate", "AGGREGATE", "Week", "tab", "view"} {
		if !strings.Contains(view, want) {
			t.Errorf("aggregate screen missing %q: %s", want, view)
		}
	}
}

func TestMessageVisibilityDefaultsAndTogglesIndependently(t *testing.T) {
	model := dashboardModel{screen: tuiLog, logMessagesVisible: true}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = updated.(dashboardModel)
	if model.logMessagesVisible {
		t.Fatal("log messages should be hidden after toggle")
	}
	model.openScreen(tuiReport)
	if model.reportMessagesVisible {
		t.Fatal("report messages should default to hidden")
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = updated.(dashboardModel)
	if !model.reportMessagesVisible || model.logMessagesVisible {
		t.Fatal("visibility settings should remain independent")
	}
}

func TestLogEditSelectionNavigatesOpensCancelsAndSavesInPlace(t *testing.T) {
	model := tuiTestModel()
	repo, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	model.service.Repo = repo
	model.screen = tuiLog
	model.rangeIndex = 4
	model.filter = "client"
	originalRange, originalFilter := model.rangeIndex, model.filter

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	model = updated.(dashboardModel)
	if !model.logEditMode || model.logEditRef != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("edit selection did not start at the first matching session: %#v", model)
	}
	if view := model.View(); !strings.Contains(view, "›") || !strings.Contains(view, "select session") {
		t.Fatalf("selected session is not indicated: %s", view)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(dashboardModel)
	if model.screen != tuiEdit || model.editForm == nil || model.editForm.project != "alpha" {
		t.Fatalf("enter did not open prepopulated editor: %#v", model)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(dashboardModel)
	if model.screen != tuiLog || model.service.Frames[0].Project != "alpha" || model.rangeIndex != originalRange || model.filter != originalFilter {
		t.Fatalf("cancel changed data or view state: %#v", model)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(dashboardModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(dashboardModel) // Begin editing Project.
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	model = updated.(dashboardModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("renamed")})
	model = updated.(dashboardModel)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(dashboardModel)
	for i := 0; i < 4; i++ {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = updated.(dashboardModel)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(dashboardModel)
	if !model.editForm.confirm {
		t.Fatal("save did not request confirmation")
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(dashboardModel)
	if model.screen != tuiLog || model.service.Frames[0].Project != "renamed" || model.filter != originalFilter || model.rangeIndex != originalRange {
		t.Fatalf("save did not refresh in place: %#v", model)
	}
	if !model.logEditMode || model.logEditRef != "aaaaaaaaaaaaaaaa" || !strings.Contains(model.editNotice, "saved") {
		t.Fatalf("save did not preserve selection/status: %#v", model)
	}
}

func TestLogEditSelectionScrollsAndStaysVisibleAfterResize(t *testing.T) {
	model := tuiTestModel()
	model.screen = tuiLog
	model.rangeIndex = 4
	model.height = 12
	for i := 0; i < 30; i++ {
		start := model.now.AddDate(0, 0, -i).Add(9 * time.Hour)
		stop := start.Add(time.Hour).Unix()
		model.service.Frames = append(model.service.Frames, store.Frame{
			Start: start.Unix(), Stop: &stop, Project: "project", ID: strings.Repeat("a", 31) + string(rune('a'+i)), Tags: []string{"T-1"},
		})
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	model = updated.(dashboardModel)
	for i := 0; i < 25; i++ {
		updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
		model = updated.(dashboardModel)
	}
	_, positions := model.logLayout()
	selectedLine := positions[model.logEditRef]
	if selectedLine < model.scroll || selectedLine >= model.scroll+model.viewportHeight() {
		t.Fatalf("selection line %d is outside scroll window [%d,%d)", selectedLine, model.scroll, model.scroll+model.viewportHeight())
	}
	updated, _ = model.Update(tea.WindowSizeMsg{Width: 64, Height: 10})
	model = updated.(dashboardModel)
	_, positions = model.logLayout()
	selectedLine = positions[model.logEditRef]
	if selectedLine < model.scroll || selectedLine >= model.scroll+model.viewportHeight() || model.scroll > model.maxScroll() {
		t.Fatalf("resize lost selected row: line=%d scroll=%d max=%d", selectedLine, model.scroll, model.maxScroll())
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
	model.cursor = 3
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

func TestFlexibleTimeFormatsDefaultToToday(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.October, 27, 16, 0, 0, 0, loc)
	cases := map[string]string{
		"2:30 PM": "2026-10-27 14:30:00", "2pm": "2026-10-27 14:00:00",
		"9am": "2026-10-27 09:00:00", "9:15 AM": "2026-10-27 09:15:00",
		"14:30": "2026-10-27 14:30:00", "09:15": "2026-10-27 09:15:00",
		"12pm": "2026-10-27 12:00:00", "12am": "2026-10-27 00:00:00",
		"2026-10-26 2:30 PM": "2026-10-26 14:30:00", "today 9am": "2026-10-27 09:00:00",
		"yesterday 2:30 PM": "2026-10-26 14:30:00", "2026-10-26 14:30": "2026-10-26 14:30:00",
	}
	for input, want := range cases {
		got, err := parseTime(input, now)
		if err != nil {
			t.Errorf("%q: %v", input, err)
			continue
		}
		if formatted := got.In(loc).Format("2006-01-02 15:04:05"); formatted != want {
			t.Errorf("%q: want %s got %s", input, want, formatted)
		}
	}
}

func TestMinutePrecisionDetectionPreservesExplicitSeconds(t *testing.T) {
	for input, want := range map[string]bool{
		"9am":                     true,
		"9:15 AM":                 true,
		"14:30":                   true,
		"2026-10-26 14:30":        true,
		"9:15:00 AM":              false,
		"2026-10-26 14:30:59":     false,
		"2026-10-26T14:30:59.250": false,
		"2026-10-26":              false,
	} {
		if got := hasMinutePrecision(input); got != want {
			t.Errorf("hasMinutePrecision(%q) = %v; want %v", input, got, want)
		}
	}
}

func TestHistoricalEditUsesOriginalDatesForTimeOnlyValues(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, loc)
	startDefault := time.Date(2026, time.October, 26, 9, 0, 0, 0, loc)
	stopDefault := time.Date(2026, time.October, 27, 0, 15, 0, 0, loc)
	parsed, err := parseEditDocumentOnDates([]byte(`{"project":"p","start":"11:30 PM","stop":"12:45 AM","tags":[]}`), true, now, startDefault, stopDefault)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Start.Format("2006-01-02 15:04:05"); got != "2026-10-26 23:30:00" {
		t.Fatalf("historical start default date = %s", got)
	}
	if got := parsed.Stop.Format("2006-01-02 15:04:05"); got != "2026-10-27 00:45:00" {
		t.Fatalf("historical stop default date = %s", got)
	}

	midnight, err := parseTime("2026-10-26 12:00 AM", now)
	if err != nil || midnight.Format("2006-01-02 15:04:05") != "2026-10-26 00:00:00" {
		t.Fatalf("midnight parse = %s, %v", midnight, err)
	}
	if got := floorMinute(time.Date(2026, time.October, 26, 0, 0, 37, 0, loc)); got.Format("2006-01-02 15:04:05") != "2026-10-26 00:00:00" {
		t.Fatalf("midnight floor = %s", got)
	}
}

func TestFlexibleTimeRejectsInvalidClocksAndKeepsDSTWallTime(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.March, 8, 12, 0, 0, 0, loc)
	for _, input := range []string{"13 PM", "0 AM", "25:00", "2026-02-30 9am"} {
		if _, err := parseTime(input, now); err == nil || !strings.Contains(err.Error(), "such as '9:30 AM'") {
			t.Errorf("%q: expected useful invalid-time error, got %v", input, err)
		}
	}
	got, err := parseTime("2:30 AM", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format("2006-01-02 15:04 MST") != "2026-03-08 02:30 EDT" {
		t.Fatalf("unexpected DST wall time: %s", got.Format("2006-01-02 15:04 MST"))
	}
}

func TestEditDocumentAcceptsFlexibleTimesForToday(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.October, 27, 16, 0, 0, 0, loc)
	parsed, err := parseEditDocument([]byte(`{"project":"p","start":"9:30 AM","stop":"11:15 AM","tags":[]}`), true, now)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Start.Format("2006-01-02 15:04") != "2026-10-27 09:30" || parsed.Stop.Format("2006-01-02 15:04") != "2026-10-27 11:15" {
		t.Fatalf("parsed start=%s stop=%s", parsed.Start, parsed.Stop)
	}
	if _, err := parseEditDocument([]byte(`{"project":"p","start":"11:00 PM","stop":"1:00 AM","tags":[]}`), true, now); err == nil || err.Error() != "Task cannot end before it starts." {
		t.Fatalf("expected overnight ambiguity error, got %v", err)
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
