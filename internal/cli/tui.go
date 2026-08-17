package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	btReport "github.com/josh/burrowtime/internal/report"
	"github.com/josh/burrowtime/internal/store"
	"github.com/josh/burrowtime/internal/watson"
)

type menuItem struct{ name, description, example string }

var menuItems = []menuItem{
	{"start", "Start tracking a project", "apollo11 +planning"},
	{"stop", "Stop one or all running timers", ""},
	{"status", "Show the running timer", ""},
	{"restart", "Restart a previous frame", "-1"},
	{"cancel", "Discard the running timer", ""},
	{"add", "Add time tracked earlier", "--from '2026-08-17 09:00' --to '2026-08-17 10:00' project"},
	{"log", "Browse and filter recorded sessions", ""},
	{"report", "Explore time grouped by project", ""},
	{"aggregate", "Aggregate a report by day", "--month"},
	{"projects", "List projects", ""},
	{"tags", "List tags", ""},
	{"frames", "List frame IDs", ""},
	{"remove", "Remove a frame", "-1"},
	{"rename", "Rename a project or tag", "project old new"},
	{"edit", "Edit a frame", "-1"},
	{"merge", "Merge a Watson frames file", "/path/to/frames"},
	{"sync", "Synchronize with a Watson server", ""},
	{"config", "Read or update configuration", "options.date_format"},
}

type tuiScreen uint8

const (
	tuiHome tuiScreen = iota
	tuiLog
	tuiReport
	tuiCommands
)

type tuiInputMode uint8

const (
	tuiInputNone tuiInputMode = iota
	tuiInputFilter
	tuiInputCommand
)

type tuiTick time.Time

type dashboardModel struct {
	service       *watson.Service
	loadErr       error
	screen        tuiScreen
	inputMode     tuiInputMode
	input         []rune
	command       menuItem
	choice        []string
	filter        string
	rangeIndex    int
	cursor        int
	commandCursor int
	scroll        int
	width         int
	height        int
	now           time.Time
	cursorVisible bool
}

var (
	tuiPrimary = lipgloss.AdaptiveColor{Light: "#0369A1", Dark: "#7DD3FC"}
	tuiAccent  = lipgloss.AdaptiveColor{Light: "#7C3AED", Dark: "#C4B5FD"}
	tuiSuccess = lipgloss.AdaptiveColor{Light: "#15803D", Dark: "#86EFAC"}
	tuiWarning = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#FCD34D"}
	tuiText    = lipgloss.AdaptiveColor{Light: "#18181B", Dark: "#F4F4F5"}
	tuiMuted   = lipgloss.AdaptiveColor{Light: "#64748B", Dark: "#94A3B8"}
	tuiBorder  = lipgloss.AdaptiveColor{Light: "#CBD5E1", Dark: "#334155"}
	tuiSurface = lipgloss.AdaptiveColor{Light: "#E2E8F0", Dark: "#1E293B"}

	tuiBrandStyle = lipgloss.NewStyle().Foreground(tuiPrimary).Bold(true)
	tuiTitleStyle = lipgloss.NewStyle().Foreground(tuiText).Bold(true)
	tuiMutedStyle = lipgloss.NewStyle().Foreground(tuiMuted)
	tuiGoodStyle  = lipgloss.NewStyle().Foreground(tuiSuccess).Bold(true)
	tuiTagStyle   = lipgloss.NewStyle().Foreground(tuiAccent)
	tuiTimeStyle  = lipgloss.NewStyle().Foreground(tuiSuccess)
	tuiIDStyle    = lipgloss.NewStyle().Foreground(tuiMuted)
)

var tuiRangeNames = []string{"Today", "Week", "Month", "Year", "All time"}

func newDashboardModel(dir string) dashboardModel {
	s, err := watson.Open(dir)
	now := time.Now()
	if s != nil {
		now = s.Now()
	}
	return dashboardModel{
		service:       s,
		loadErr:       err,
		screen:        tuiHome,
		rangeIndex:    1,
		width:         90,
		height:        28,
		now:           now,
		cursorVisible: true,
	}
}

func (m dashboardModel) Init() tea.Cmd { return tuiTickCommand() }

func tuiTickCommand() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return tuiTick(t) })
}

func (m dashboardModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampScroll()
		return m, nil
	case tuiTick:
		m.now = time.Time(msg)
		m.cursorVisible = !m.cursorVisible
		return m, tuiTickCommand()
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollBy(-3)
		case tea.MouseButtonWheelDown:
			m.scrollBy(3)
		}
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.inputMode != tuiInputNone {
			return m.updateInput(msg)
		}
		return m.updateScreen(msg)
	}
	return m, nil
}

func (m dashboardModel) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.inputMode = tuiInputNone
		return m, nil
	case tea.KeyEnter:
		if m.inputMode == tuiInputFilter {
			m.filter = strings.TrimSpace(string(m.input))
			m.inputMode = tuiInputNone
			m.scroll = 0
			return m, nil
		}
		m.choice = append([]string{m.command.name}, splitCommandLine(string(m.input))...)
		return m, tea.Quit
	case tea.KeyBackspace, tea.KeyDelete:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	case tea.KeySpace:
		m.input = append(m.input, ' ')
	case tea.KeyRunes:
		m.input = append(m.input, msg.Runes...)
	}
	if m.inputMode == tuiInputFilter {
		m.filter = strings.TrimSpace(string(m.input))
		m.scroll = 0
	}
	return m, nil
}

func (m dashboardModel) updateScreen(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "q" {
		return m, tea.Quit
	}
	switch m.screen {
	case tuiHome:
		switch key {
		case "up", "k":
			m.cursor = wrapIndex(m.cursor-1, 5)
		case "down", "j":
			m.cursor = wrapIndex(m.cursor+1, 5)
		case "enter":
			return m.activateQuickAction()
		case "l":
			m.openScreen(tuiLog)
		case "r":
			m.openScreen(tuiReport)
		case "s":
			m.beginCommand(menuItems[0])
		case "x":
			m.choice = []string{"stop"}
			return m, tea.Quit
		case "c", ":":
			m.openScreen(tuiCommands)
		}
	case tuiCommands:
		switch key {
		case "esc", "b":
			m.openScreen(tuiHome)
		case "up", "k":
			m.commandCursor = wrapIndex(m.commandCursor-1, len(menuItems))
		case "down", "j":
			m.commandCursor = wrapIndex(m.commandCursor+1, len(menuItems))
		case "enter":
			return m.activateMenuItem(menuItems[m.commandCursor])
		}
	case tuiLog, tuiReport:
		switch key {
		case "esc", "b":
			m.openScreen(tuiHome)
		case "tab":
			if m.screen == tuiLog {
				m.openScreen(tuiReport)
			} else {
				m.openScreen(tuiLog)
			}
		case "left", "h":
			m.rangeIndex = wrapIndex(m.rangeIndex-1, len(tuiRangeNames))
			m.scroll = 0
		case "right", "l":
			m.rangeIndex = wrapIndex(m.rangeIndex+1, len(tuiRangeNames))
			m.scroll = 0
		case "/":
			m.inputMode = tuiInputFilter
			m.input = []rune(m.filter)
		case "c":
			m.filter = ""
			m.scroll = 0
		case "up", "k":
			m.scrollBy(-1)
		case "down", "j":
			m.scrollBy(1)
		case "pgup", "ctrl+u":
			m.scrollBy(-m.viewportHeight() / 2)
		case "pgdown", "ctrl+d":
			m.scrollBy(m.viewportHeight() / 2)
		case "g", "home":
			m.scroll = 0
		case "G", "end":
			m.scroll = m.maxScroll()
		}
	}
	return m, nil
}

func (m *dashboardModel) activateQuickAction() (tea.Model, tea.Cmd) {
	switch m.cursor {
	case 0:
		m.openScreen(tuiLog)
	case 1:
		m.openScreen(tuiReport)
	case 2:
		m.beginCommand(menuItems[0])
	case 3:
		m.choice = []string{"stop"}
		return *m, tea.Quit
	case 4:
		m.openScreen(tuiCommands)
	}
	return *m, nil
}

func (m *dashboardModel) activateMenuItem(item menuItem) (tea.Model, tea.Cmd) {
	switch item.name {
	case "log":
		m.openScreen(tuiLog)
		return *m, nil
	case "report":
		m.openScreen(tuiReport)
		return *m, nil
	}
	if item.example == "" {
		m.choice = []string{item.name}
		return *m, tea.Quit
	}
	m.beginCommand(item)
	return *m, nil
}

func (m *dashboardModel) beginCommand(item menuItem) {
	m.command = item
	m.inputMode = tuiInputCommand
	m.input = nil
}

func (m *dashboardModel) openScreen(screen tuiScreen) {
	m.screen = screen
	m.inputMode = tuiInputNone
	m.scroll = 0
}

func (m dashboardModel) View() string {
	if m.width < 1 {
		m.width = 90
	}
	if m.height < 1 {
		m.height = 28
	}
	if m.loadErr != nil {
		return m.errorView()
	}
	if m.inputMode == tuiInputCommand {
		return m.commandInputView()
	}
	var body string
	switch m.screen {
	case tuiLog:
		body = m.logView()
	case tuiReport:
		body = m.reportView()
	case tuiCommands:
		body = m.commandsView()
	default:
		body = m.homeView()
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(body)
}

func (m dashboardModel) header(section string) string {
	brand := tuiBrandStyle.Render("◷  BURROWTIME")
	if section != "" {
		brand += tuiMutedStyle.Render("  /  ") + tuiTitleStyle.Render(section)
	}
	status := tuiMutedStyle.Render("idle")
	if count := len(m.service.RunningTimers()); count > 0 {
		status = tuiGoodStyle.Render(fmt.Sprintf("● %d tracking", count))
	}
	return padBetween(brand, status, m.contentWidth())
}

func (m dashboardModel) homeView() string {
	width := m.contentWidth()
	timers := m.service.RunningTimers()
	statusLine := tuiMutedStyle.Render("Nothing running — choose Start when you're ready.")
	if len(timers) == 1 {
		timer := timers[0]
		elapsed := max(time.Duration(0), m.now.Sub(time.Unix(timer.Start, 0)))
		project := tuiTitleStyle.Foreground(tuiAccent).Render(timer.Project)
		tags := ""
		if len(timer.Tags) > 0 {
			tags = "  " + tuiTagStyle.Render("#"+strings.Join(timer.Tags, "  #"))
		}
		statusLine = tuiGoodStyle.Render("● NOW") + "  " + project + tags + "\n" +
			tuiTimeStyle.Bold(true).Render(formatDuration(int64(elapsed/time.Second))) + tuiMutedStyle.Render("  elapsed")
	} else if len(timers) > 1 {
		lines := []string{tuiGoodStyle.Render(fmt.Sprintf("● %d TIMERS RUNNING", len(timers)))}
		for _, timer := range timers[:min(3, len(timers))] {
			elapsed := max(time.Duration(0), m.now.Sub(time.Unix(timer.Start, 0)))
			projectWidth := max(10, width-31)
			left := tuiIDStyle.Render(fmt.Sprintf("%-7s", shortID(timer.ID))) + "  " + tuiTitleStyle.Foreground(tuiAccent).Render(truncate(timer.Project, projectWidth))
			lines = append(lines, left+"  "+tuiTimeStyle.Render(formatDuration(int64(elapsed/time.Second))))
		}
		if len(timers) > 3 {
			lines = append(lines, tuiMutedStyle.Render(fmt.Sprintf("…and %d more", len(timers)-3)))
		}
		statusLine = strings.Join(lines, "\n")
	}
	today := m.totalForRange(0)
	week := m.totalForRange(1)
	metrics := tuiMutedStyle.Render("TODAY  ") + tuiTimeStyle.Render(formatDuration(int64(today))) +
		"     " + tuiMutedStyle.Render("THIS WEEK  ") + tuiTimeStyle.Render(formatDuration(int64(week))) +
		"     " + tuiMutedStyle.Render("SESSIONS  ") + tuiTitleStyle.Render(fmt.Sprint(len(m.service.Frames)))
	hero := tuiPanel("CURRENT TIMER", statusLine+"\n\n"+metrics, width)

	actions := []struct{ label, detail string }{
		{"Log", "Browse individual sessions"},
		{"Report", "Compare projects and totals"},
		{"Start", "Track a project and tags"},
		{"Stop", "Finish one or all timers"},
		{"Commands", "Open every Watson command"},
	}
	actionWidth := max(32, width/2)
	actionInnerWidth := max(20, actionWidth-5)
	shortcuts := []string{"l", "r", "s", "x", "c"}
	var actionLines []string
	for i, action := range actions {
		marker := "  "
		label := tuiTitleStyle.Render(action.label)
		right := tuiMutedStyle.Render("[" + shortcuts[i] + "]")
		if actionWidth >= 50 {
			right = tuiMutedStyle.Render(action.detail)
		}
		if i == m.cursor {
			marker = tuiBrandStyle.Render("› ")
			label = lipgloss.NewStyle().Foreground(tuiPrimary).Bold(true).Render(action.label)
		}
		if actionWidth < 50 {
			actionLines = append(actionLines, marker+fmt.Sprintf("%-12s", action.label)+right)
		} else {
			actionLines = append(actionLines, marker+padBetween(label, right, actionInnerWidth-4))
		}
	}
	recentWidth := max(32, width-actionWidth-1)
	actionsPanel := tuiPanel("QUICK ACTIONS", strings.Join(actionLines, "\n"), actionWidth)
	recentPanel := tuiPanel("RECENT", m.recentLines(max(20, recentWidth-7)), recentWidth)

	main := actionsPanel
	if width >= 76 {
		main = lipgloss.JoinHorizontal(lipgloss.Top, actionsPanel, " ", recentPanel)
	} else if m.height >= 30 {
		main += "\n" + recentPanel
	}
	footer := tuiHelp("↑/↓", "move", "enter", "open", "l/r", "views", "s", "start", "q", "quit")
	if width < 90 {
		footer = tuiHelp("↑/↓", "navigate", "enter", "open", "q", "quit")
	}
	return m.header("") + "\n\n" + hero + "\n" + main + "\n" + footer
}

func (m dashboardModel) recentLines(width int) string {
	frames := append([]store.Frame(nil), m.service.Frames...)
	sort.SliceStable(frames, func(i, j int) bool { return frames[i].Start > frames[j].Start })
	if len(frames) == 0 {
		return tuiMutedStyle.Render("No sessions yet. Your first burrow awaits.")
	}
	count := min(5, len(frames))
	lines := make([]string, 0, count)
	loc := watsonDisplayLocation(m.now)
	for _, frame := range frames[:count] {
		when := time.Unix(frame.Start, 0).In(loc).Format("Mon 15:04")
		duration := int64(0)
		if frame.Stop != nil {
			duration = *frame.Stop - frame.Start
		}
		left := tuiMutedStyle.Render(when) + "  " + tuiTagStyle.Render(truncate(frame.Project, max(8, width-25)))
		lines = append(lines, padBetween(left, tuiTimeStyle.Render(formatDuration(duration)), width))
	}
	return strings.Join(lines, "\n")
}

func (m dashboardModel) logView() string {
	lines := m.logLines()
	toolbar := m.queryToolbar("LOG", len(m.tuiFrames(false)), "sessions")
	content, position := m.viewport(lines)
	footer := m.queryFooter("report")
	return m.header("Log") + "\n\n" + toolbar + "\n\n" + content + "\n" + padBetween(footer, position, m.contentWidth())
}

func (m dashboardModel) logLines() []string {
	frames := m.tuiFrames(false)
	if len(frames) == 0 {
		return []string{"", tuiMutedStyle.Render("  No sessions match this range and filter.")}
	}
	loc := watsonDisplayLocation(m.now)
	byDay := map[string][]store.Frame{}
	var days []string
	for _, frame := range frames {
		day := time.Unix(frame.Start, 0).In(loc).Format("2006-01-02")
		if _, exists := byDay[day]; !exists {
			days = append(days, day)
		}
		byDay[day] = append(byDay[day], frame)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))
	width := m.contentWidth()
	var lines []string
	for dayIndex, day := range days {
		if dayIndex > 0 {
			lines = append(lines, "")
		}
		dayFrames := byDay[day]
		sort.SliceStable(dayFrames, func(i, j int) bool { return dayFrames[i].Start < dayFrames[j].Start })
		var total int64
		for _, frame := range dayFrames {
			if frame.Stop != nil {
				total += *frame.Stop - frame.Start
			}
		}
		date, _ := time.ParseInLocation("2006-01-02", day, loc)
		dayTitle := tuiBrandStyle.Render(date.Format("Monday, 02 January 2006"))
		lines = append(lines, padBetween(dayTitle, tuiTimeStyle.Render(formatDuration(total)), width))
		lines = append(lines, tuiMutedStyle.Render(strings.Repeat("─", max(1, width))))
		for _, frame := range dayFrames {
			start := time.Unix(frame.Start, 0).In(loc)
			stop := m.now.In(loc)
			if frame.Stop != nil {
				stop = time.Unix(*frame.Stop, 0).In(loc)
			}
			duration := int64(stop.Sub(start) / time.Second)
			tags := ""
			if len(frame.Tags) > 0 {
				tags = "#" + strings.Join(frame.Tags, " #")
			}
			if width < 72 {
				projectWidth := max(8, width-32)
				line := "  " + tuiTimeStyle.Render(start.Format("15:04")+"–"+stop.Format("15:04")) + "  " +
					tuiTagStyle.Render(truncate(frame.Project, projectWidth))
				lines = append(lines, padBetween(line, tuiMutedStyle.Render(formatDuration(duration)), width))
				if tags != "" {
					lines = append(lines, "                "+tuiMutedStyle.Render(truncate(tags, max(1, width-16))))
				}
				continue
			}
			projectWidth := min(22, max(12, width/5))
			tagsWidth := max(8, width-50-projectWidth)
			id := shortID(frame.ID)
			if strings.HasPrefix(frame.ID, "current") {
				id = "running"
			}
			line := "  " + tuiIDStyle.Render(fmt.Sprintf("%-7s", id)) + "  " +
				tuiTimeStyle.Render(start.Format("15:04")+" → "+stop.Format("15:04")) + "  " +
				tuiMutedStyle.Render(fmt.Sprintf("%11s", formatDuration(duration))) + "  " +
				tuiTagStyle.Render(fmt.Sprintf("%-*s", projectWidth, truncate(frame.Project, projectWidth)))
			if tags != "" {
				line += "  " + tuiMutedStyle.Render(truncate(tags, tagsWidth))
			}
			lines = append(lines, line)
		}
	}
	return lines
}

func (m dashboardModel) reportView() string {
	frames := m.tuiFrames(true)
	options, _ := m.tuiOptions(true)
	summary := btReport.Build(frames, options)
	lines := m.reportLines(summary)
	toolbar := m.queryToolbar("REPORT", len(summary.Projects), "projects")
	content, position := m.viewport(lines)
	footer := m.queryFooter("log")
	return m.header("Report") + "\n\n" + toolbar + "\n\n" + content + "\n" + padBetween(footer, position, m.contentWidth())
}

func (m dashboardModel) reportLines(summary btReport.Summary) []string {
	if len(summary.Projects) == 0 {
		return []string{"", tuiMutedStyle.Render("  No tracked time matches this range and filter.")}
	}
	projects := append([]btReport.ProjectTotal(nil), summary.Projects...)
	sort.SliceStable(projects, func(i, j int) bool { return projects[i].Time > projects[j].Time })
	width := m.contentWidth()
	barWidth := min(30, max(8, width-48))
	var lines []string
	total := float64(summary.Time)
	span := summary.Timespan.From.Format("02 Jan 2006") + "  →  " + summary.Timespan.To.Format("02 Jan 2006")
	lines = append(lines, padBetween(tuiMutedStyle.Render(span), tuiTimeStyle.Bold(true).Render(formatDuration(int64(total))), width), "")
	for _, project := range projects {
		percent := 0.0
		if total > 0 {
			percent = float64(project.Time) / total
		}
		filled := int(percent * float64(barWidth))
		if filled == 0 && project.Time > 0 {
			filled = 1
		}
		bar := lipgloss.NewStyle().Foreground(tuiPrimary).Render(strings.Repeat("━", filled)) +
			lipgloss.NewStyle().Foreground(tuiSurface).Render(strings.Repeat("━", max(0, barWidth-filled)))
		nameWidth := min(24, max(10, width-barWidth-25))
		line := "  " + tuiTitleStyle.Foreground(tuiAccent).Render(fmt.Sprintf("%-*s", nameWidth, truncate(project.Name, nameWidth))) + "  " + bar
		right := tuiTimeStyle.Render(fmt.Sprintf("%11s", formatDuration(int64(project.Time)))) + tuiMutedStyle.Render(fmt.Sprintf("  %3.0f%%", percent*100))
		lines = append(lines, padBetween(line, right, width))
		if len(project.Tags) > 0 {
			parts := make([]string, 0, len(project.Tags))
			for _, tag := range project.Tags {
				parts = append(parts, "#"+tag.Name+" "+formatDuration(int64(tag.Time)))
			}
			lines = append(lines, "    "+tuiMutedStyle.Render(truncate(strings.Join(parts, "   "), max(1, width-4))))
		}
		lines = append(lines, "")
	}
	return lines
}

func (m dashboardModel) queryToolbar(kind string, count int, noun string) string {
	rangeBadge := tuiBadge("‹  " + tuiRangeNames[m.rangeIndex] + "  ›")
	filterBadge := tuiMutedStyle.Render("/ filter")
	if m.filter != "" {
		filterBadge = tuiTagStyle.Render("filter: "+truncate(m.filter, 28)) + tuiMutedStyle.Render("  (c clears)")
	}
	if m.inputMode == tuiInputFilter {
		cursor := ""
		if m.cursorVisible {
			cursor = "█"
		}
		filterBadge = tuiTagStyle.Render("/ "+string(m.input)) + tuiBrandStyle.Render(cursor)
	}
	left := tuiBrandStyle.Render(kind) + "  " + rangeBadge + "  " + filterBadge
	right := tuiMutedStyle.Render(fmt.Sprintf("%d %s", count, noun))
	return padBetween(left, right, m.contentWidth())
}

func (m dashboardModel) queryFooter(otherView string) string {
	if m.contentWidth() < 96 {
		return tuiHelp("←/→", "range", "/", "filter", "j/k", "scroll", "b", "back")
	}
	return tuiHelp("←/→", "range", "/", "filter", "j/k", "scroll", "tab", otherView, "b", "back", "q", "quit")
}

func (m dashboardModel) commandsView() string {
	width := m.contentWidth()
	height := max(5, m.height-8)
	start := m.commandCursor - height/2
	if start < 0 {
		start = 0
	}
	end := min(len(menuItems), start+height)
	if end == len(menuItems) {
		start = max(0, end-height)
	}
	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		item := menuItems[i]
		marker := "  "
		name := tuiTitleStyle.Render(fmt.Sprintf("%-11s", item.name))
		detail := tuiMutedStyle.Render(truncate(item.description, max(12, width-20)))
		if i == m.commandCursor {
			marker = tuiBrandStyle.Render("› ")
			name = lipgloss.NewStyle().Foreground(tuiPrimary).Bold(true).Render(fmt.Sprintf("%-11s", item.name))
		}
		lines = append(lines, marker+name+"  "+detail)
	}
	footer := tuiHelp("↑/↓", "move", "enter", "open", "b", "back", "q", "quit")
	return m.header("Commands") + "\n\n" + tuiPanel("ALL COMMANDS", strings.Join(lines, "\n"), width) + "\n" + footer
}

func (m dashboardModel) commandInputView() string {
	width := m.contentWidth()
	cursor := ""
	if m.cursorVisible {
		cursor = "█"
	}
	detail := tuiMutedStyle.Render(m.command.description) + "\n\n"
	if m.command.example != "" {
		detail += tuiMutedStyle.Render("Example  ") + tuiTagStyle.Render(m.command.example) + "\n\n"
	}
	prompt := tuiBrandStyle.Render(m.command.name+"  ") + tuiTitleStyle.Render(string(m.input)) + tuiBrandStyle.Render(cursor)
	body := detail + prompt
	footer := tuiHelp("enter", "run command", "esc", "cancel")
	return lipgloss.NewStyle().Padding(1, 2).Render(m.header("Command") + "\n\n" + tuiPanel("COMMAND ARGUMENTS", body, width) + "\n" + footer)
}

func (m dashboardModel) errorView() string {
	width := m.contentWidth()
	message := lipgloss.NewStyle().Foreground(tuiWarning).Bold(true).Render("Could not load BurrowTime data") + "\n\n" + tuiMutedStyle.Render(m.loadErr.Error())
	return lipgloss.NewStyle().Padding(1, 2).Render(m.header("Error") + "\n\n" + tuiPanel("STARTUP ERROR", message, width) + "\n" + tuiHelp("q", "quit"))
}

func (m dashboardModel) tuiOptions(reportMode bool) (btReport.Options, error) {
	q := queryFlags{current: true}
	switch m.rangeIndex {
	case 0:
		q.day = true
	case 1:
		q.week = true
	case 2:
		q.month = true
	case 3:
		q.year = true
	case 4:
		q.all = true
	}
	return q.options(m.service, reportMode)
}

func (m dashboardModel) tuiFrames(reportMode bool) []store.Frame {
	options, err := m.tuiOptions(reportMode)
	if err != nil {
		return nil
	}
	frames := btReport.FilterActive(m.service.Frames, m.service.RunningTimers(), m.now, options)
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return frames
	}
	loc := watsonDisplayLocation(m.now)
	filtered := make([]store.Frame, 0, len(frames))
	for _, frame := range frames {
		haystack := strings.ToLower(strings.Join([]string{
			frame.ID,
			frame.Project,
			strings.Join(frame.Tags, " "),
			time.Unix(frame.Start, 0).In(loc).Format("Monday 02 January 2006 15:04"),
		}, " "))
		if strings.Contains(haystack, query) {
			filtered = append(filtered, frame)
		}
	}
	return filtered
}

func (m dashboardModel) totalForRange(index int) btReport.Seconds {
	m.rangeIndex = index
	options, err := m.tuiOptions(true)
	if err != nil {
		return 0
	}
	frames := btReport.FilterActive(m.service.Frames, m.service.RunningTimers(), m.now, options)
	return btReport.Build(frames, options).Time
}

func (m dashboardModel) viewport(lines []string) (string, string) {
	height := m.viewportHeight()
	maxScroll := max(0, len(lines)-height)
	start := min(max(0, m.scroll), maxScroll)
	end := min(len(lines), start+height)
	visible := append([]string(nil), lines[start:end]...)
	for len(visible) < height {
		visible = append(visible, "")
	}
	position := ""
	if len(lines) > height {
		position = tuiMutedStyle.Render(fmt.Sprintf("%d–%d / %d", start+1, end, len(lines)))
	}
	return strings.Join(visible, "\n"), position
}

func (m dashboardModel) viewportHeight() int { return max(4, m.height-9) }

func (m dashboardModel) currentLineCount() int {
	if m.screen == tuiLog {
		return len(m.logLines())
	}
	if m.screen == tuiReport {
		options, _ := m.tuiOptions(true)
		summary := btReport.Build(m.tuiFrames(true), options)
		return len(m.reportLines(summary))
	}
	return 0
}

func (m dashboardModel) maxScroll() int {
	return max(0, m.currentLineCount()-m.viewportHeight())
}

func (m *dashboardModel) scrollBy(delta int) {
	m.scroll += delta
	m.clampScroll()
}

func (m *dashboardModel) clampScroll() {
	m.scroll = min(max(0, m.scroll), m.maxScroll())
}

func (m dashboardModel) contentWidth() int { return max(32, min(118, m.width-4)) }

func tuiPanel(title, body string, width int) string {
	width = max(20, width)
	heading := tuiMutedStyle.Bold(true).Render(title)
	return lipgloss.NewStyle().
		Width(max(1, width-4)).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(tuiBorder).
		Render(heading + "\n" + body)
}

func tuiBadge(value string) string {
	return lipgloss.NewStyle().Foreground(tuiPrimary).Background(tuiSurface).Padding(0, 1).Render(value)
}

func tuiHelp(parts ...string) string {
	var rendered []string
	for i := 0; i+1 < len(parts); i += 2 {
		rendered = append(rendered, tuiTitleStyle.Foreground(tuiPrimary).Render(parts[i])+" "+tuiMutedStyle.Render(parts[i+1]))
	}
	return strings.Join(rendered, tuiMutedStyle.Render("  •  "))
}

func padBetween(left, right string, width int) string {
	space := width - lipgloss.Width(left) - lipgloss.Width(right)
	if space < 1 {
		space = 1
	}
	return left + strings.Repeat(" ", space) + right
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}

func wrapIndex(value, length int) int {
	if length <= 0 {
		return 0
	}
	value %= length
	if value < 0 {
		value += length
	}
	return value
}

const stopAllSelection = "__all__"

type stopPickerModel struct {
	timers    []store.ActiveTimer
	now       time.Time
	cursor    int
	selection string
	canceled  bool
	width     int
}

func (m stopPickerModel) Init() tea.Cmd { return nil }

func (m stopPickerModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.canceled = true
			return m, tea.Quit
		case "up", "k":
			m.cursor = wrapIndex(m.cursor-1, len(m.timers)+1)
		case "down", "j":
			m.cursor = wrapIndex(m.cursor+1, len(m.timers)+1)
		case "a":
			m.selection = stopAllSelection
			return m, tea.Quit
		case "enter":
			if m.cursor == len(m.timers) {
				m.selection = stopAllSelection
			} else {
				m.selection = m.timers[m.cursor].ID
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m stopPickerModel) View() string {
	width := max(44, min(86, m.width-4))
	if m.width == 0 {
		width = 72
	}
	lines := make([]string, 0, len(m.timers)+2)
	for i, timer := range m.timers {
		marker := "  "
		projectStyle := tuiTitleStyle
		if i == m.cursor {
			marker = tuiBrandStyle.Render("› ")
			projectStyle = tuiTitleStyle.Foreground(tuiPrimary)
		}
		elapsed := m.now.Sub(time.Unix(timer.Start, 0))
		if elapsed < 0 {
			elapsed = 0
		}
		id := shortID(timer.ID)
		projectWidth := max(10, width-32)
		left := marker + tuiIDStyle.Render(fmt.Sprintf("%-7s", id)) + "  " + projectStyle.Render(truncate(timer.Project, projectWidth))
		right := tuiTimeStyle.Render(formatDuration(int64(elapsed / time.Second)))
		lines = append(lines, left+"  "+right)
		if len(timer.Tags) > 0 {
			lines = append(lines, "             "+tuiTagStyle.Render("#"+strings.Join(timer.Tags, "  #")))
		}
	}
	marker := "  "
	label := tuiMutedStyle.Render("Stop all running timers")
	if m.cursor == len(m.timers) {
		marker = tuiBrandStyle.Render("› ")
		label = lipgloss.NewStyle().Foreground(tuiWarning).Bold(true).Render("Stop all running timers")
	}
	lines = append(lines, marker+label)
	body := tuiMutedStyle.Render("Several timers are active. Choose which one to finish.") + "\n\n" + strings.Join(lines, "\n")
	footer := tuiHelp("↑/↓", "move", "enter", "stop", "a", "stop all", "esc", "cancel")
	return lipgloss.NewStyle().Padding(1, 2).Render(tuiBrandStyle.Render("◷  BURROWTIME") + "\n\n" + tuiPanel("STOP A TIMER", body, width) + "\n" + footer)
}

func runStopPicker(timers []store.ActiveTimer, now time.Time) (string, error) {
	model := stopPickerModel{timers: append([]store.ActiveTimer(nil), timers...), now: now, width: 76}
	result, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	if err != nil {
		return "", err
	}
	final := result.(stopPickerModel)
	if final.canceled || final.selection == "" {
		return "", errors.New("Aborted!")
	}
	return final.selection, nil
}

func runDashboard(dir string) ([]string, error) {
	model := newDashboardModel(dir)
	result, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	if err != nil {
		return nil, err
	}
	return result.(dashboardModel).choice, nil
}

// runPalette is kept as a small compatibility wrapper for callers and tests
// from the original command-palette implementation.
func runPalette() ([]string, error) { return runDashboard("") }

// splitCommandLine provides the quoting users need for multi-word dates and
// projects in the command launcher. It intentionally mirrors shell quoting.
func splitCommandLine(value string) []string {
	var out []string
	var word strings.Builder
	var quote rune
	escaped := false
	started := false
	flush := func() {
		if started {
			out = append(out, word.String())
			word.Reset()
			started = false
		}
	}
	for _, r := range value {
		if escaped {
			word.WriteRune(r)
			escaped = false
			started = true
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			started = true
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			started = true
			continue
		}
		if r == ' ' || r == '\t' {
			flush()
			continue
		}
		word.WriteRune(r)
		started = true
	}
	flush()
	return out
}
