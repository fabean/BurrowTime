package cli

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	btReport "github.com/fabean/BurrowTime/internal/report"
	"github.com/fabean/BurrowTime/internal/store"
	"github.com/fabean/BurrowTime/internal/watson"
)

type menuItem struct{ name, description, example string }

var menuItems = []menuItem{
	{"start", "Start tracking a project", "apollo11 +planning"},
	{"stop", "Stop one or all running timers", ""},
	{"status", "Show the running timer", ""},
	{"restart", "Restart a previous frame", "-1"},
	{"cancel", "Discard the running timer", ""},
	{"add", "Add time tracked earlier", "--from '9:00 AM' --to '10:00 AM' project"},
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
	tuiAggregate
	tuiCommands
	tuiEdit
)

type tuiInputMode uint8

const (
	tuiInputNone tuiInputMode = iota
	tuiInputFilter
	tuiInputCommand
)

type tuiTick time.Time

type dashboardModel struct {
	service                  *watson.Service
	loadErr                  error
	screen                   tuiScreen
	inputMode                tuiInputMode
	input                    []rune
	command                  menuItem
	choice                   []string
	filter                   string
	rangeIndex               int
	cursor                   int
	commandCursor            int
	scroll                   int
	width                    int
	height                   int
	now                      time.Time
	cursorVisible            bool
	logMessagesVisible       bool
	reportMessagesVisible    bool
	aggregateMessagesVisible bool
	logEditMode              bool
	logEditCursor            int
	logEditRef               string
	editForm                 *sessionEditModel
	editNotice               string
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

func nextTUIView(screen tuiScreen) tuiScreen {
	switch screen {
	case tuiLog:
		return tuiReport
	case tuiReport:
		return tuiAggregate
	default:
		return tuiLog
	}
}

func newDashboardModel(dir string) dashboardModel {
	s, err := watson.Open(dir)
	if err == nil {
		_, err = s.RecoverExpiredAgentSessions()
	}
	now := time.Now()
	if s != nil {
		now = s.Now()
	}
	return dashboardModel{
		service:            s,
		loadErr:            err,
		screen:             tuiHome,
		rangeIndex:         1,
		width:              90,
		height:             28,
		now:                now,
		cursorVisible:      true,
		logMessagesVisible: true,
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
		if m.editForm != nil {
			m.editForm.width = msg.Width
		}
		m.clampScroll()
		if m.screen == tuiLog && m.logEditMode {
			m.keepLogEditSelectionVisible()
		}
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
		if m.screen == tuiEdit {
			return m.updateEdit(msg)
		}
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
			m.cursor = wrapIndex(m.cursor-1, 6)
		case "down", "j":
			m.cursor = wrapIndex(m.cursor+1, 6)
		case "enter":
			return m.activateQuickAction()
		case "l":
			m.openScreen(tuiLog)
		case "r":
			m.openScreen(tuiReport)
		case "a":
			m.openScreen(tuiAggregate)
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
	case tuiLog, tuiReport, tuiAggregate:
		if m.screen == tuiLog && m.logEditMode {
			switch key {
			case "esc":
				m.logEditMode = false
				m.logEditRef = ""
			case "up", "k":
				m.moveLogEditSelection(-1)
			case "down", "j":
				m.moveLogEditSelection(1)
			case "enter":
				m.openLogEditForm()
			}
			return m, nil
		}
		if key == "m" {
			switch m.screen {
			case tuiLog:
				m.logMessagesVisible = !m.logMessagesVisible
				m.scroll = 0
			case tuiReport:
				m.reportMessagesVisible = !m.reportMessagesVisible
				m.scroll = 0
			case tuiAggregate:
				m.aggregateMessagesVisible = !m.aggregateMessagesVisible
				m.clampScroll()
			}
			return m, nil
		}
		if key == "a" {
			m.openScreen(tuiAggregate)
			return m, nil
		}
		switch key {
		case "e":
			if m.screen == tuiLog {
				m.startLogEditSelection()
			}
		case "esc", "b":
			m.openScreen(tuiHome)
		case "tab":
			m.openScreen(nextTUIView(m.screen))
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
		m.openScreen(tuiAggregate)
	case 3:
		m.beginCommand(menuItems[0])
	case 4:
		m.choice = []string{"stop"}
		return *m, tea.Quit
	case 5:
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
	case "aggregate":
		m.openScreen(tuiAggregate)
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
	case tuiAggregate:
		body = m.aggregateView()
	case tuiCommands:
		body = m.commandsView()
	case tuiEdit:
		if m.editForm != nil {
			body = m.editForm.View()
		}
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
		{"Aggregate", "Compare daily project and ticket totals"},
		{"Start", "Track a project and tags"},
		{"Stop", "Finish one or all timers"},
		{"Commands", "Open every Watson command"},
	}
	actionWidth := max(32, width/2)
	actionInnerWidth := max(20, actionWidth-5)
	shortcuts := []string{"l", "r", "a", "s", "x", "c"}
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
	footer := tuiHelp("↑/↓", "move", "enter", "open", "l/r/a", "views", "s", "start", "q", "quit")
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
	footer := m.queryFooter()
	if m.logEditMode {
		footer = tuiHelp("↑/↓ or j/k", "select session", "enter", "edit", "esc", "cancel")
	} else {
		footer += tuiMutedStyle.Render("  m messages " + visibilityLabel(m.logMessagesVisible) + "  e edit session")
	}
	if m.editNotice != "" {
		footer += "  " + tuiMutedStyle.Render(m.editNotice)
	}
	return m.header("Log") + "\n\n" + toolbar + "\n\n" + content + "\n" + padBetween(footer, position, m.contentWidth())
}

func (m dashboardModel) logLines() []string {
	lines, _ := m.logLayout()
	return lines
}

func (m dashboardModel) logLayout() ([]string, map[string]int) {
	frames := m.logDisplayFrames()
	if len(frames) == 0 {
		return []string{"", tuiMutedStyle.Render("  No sessions match this range and filter.")}, map[string]int{}
	}
	loc := watsonDisplayLocation(m.now)
	byDay := map[string][]editSessionOption{}
	var days []string
	for _, option := range frames {
		frame := option.frame
		day := time.Unix(frame.Start, 0).In(loc).Format("2006-01-02")
		if _, exists := byDay[day]; !exists {
			days = append(days, day)
		}
		byDay[day] = append(byDay[day], option)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))
	width := m.contentWidth()
	var lines []string
	positions := make(map[string]int, len(frames))
	for dayIndex, day := range days {
		if dayIndex > 0 {
			lines = append(lines, "")
		}
		dayFrames := byDay[day]
		var total int64
		for _, option := range dayFrames {
			frame := option.frame
			if frame.Stop != nil {
				total += *frame.Stop - frame.Start
			}
		}
		date, _ := time.ParseInLocation("2006-01-02", day, loc)
		dayTitle := tuiBrandStyle.Render(date.Format("Monday, 02 January 2006"))
		lines = append(lines, padBetween(dayTitle, tuiTimeStyle.Render(formatDuration(total)), width))
		lines = append(lines, tuiMutedStyle.Render(strings.Repeat("─", max(1, width))))
		for _, option := range dayFrames {
			frame := option.frame
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
				if option.ref != "" {
					positions[option.ref] = len(lines)
				}
				if m.logEditMode && option.ref != "" && option.ref == m.logEditRef {
					line = lipgloss.NewStyle().Foreground(tuiPrimary).Background(tuiSurface).Bold(true).Render("› " + strings.TrimLeft(line, " "))
				}
				lines = append(lines, padBetween(line, tuiMutedStyle.Render(formatDuration(duration)), width))
				if tags != "" {
					lines = append(lines, "                "+tuiMutedStyle.Render(truncate(tags, max(1, width-16))))
				}
				if m.logMessagesVisible && frame.Message != "" {
					lines = append(lines, "                  "+tuiMutedStyle.Render("↳ "+frame.Message))
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
			if option.ref != "" {
				positions[option.ref] = len(lines)
			}
			if m.logEditMode && option.ref != "" && option.ref == m.logEditRef {
				line = lipgloss.NewStyle().Foreground(tuiPrimary).Background(tuiSurface).Bold(true).Render("› " + strings.TrimLeft(line, " "))
			}
			lines = append(lines, line)
			if m.logMessagesVisible && frame.Message != "" {
				lines = append(lines, "      "+tuiMutedStyle.Render("↳ "+frame.Message))
			}
		}
	}
	return lines, positions
}

func (m dashboardModel) logDisplayFrames() []editSessionOption {
	frames := m.tuiFrames(false)
	loc := watsonDisplayLocation(m.now)
	sort.SliceStable(frames, func(i, j int) bool {
		a := time.Unix(frames[i].Start, 0).In(loc).Format("2006-01-02")
		b := time.Unix(frames[j].Start, 0).In(loc).Format("2006-01-02")
		if a != b {
			return a > b
		}
		return frames[i].Start < frames[j].Start
	})
	used := map[int]bool{}
	options := make([]editSessionOption, 0, len(frames))
	for _, frame := range frames {
		ref := frame.ID
		index := -1
		if frame.ID != "" {
			for i, original := range m.service.Frames {
				if original.ID == frame.ID {
					index = i
					break
				}
			}
		} else if frame.Stop != nil {
			for i, original := range m.service.Frames {
				if !used[i] && sameFrameIdentity(original, frame) {
					index = i
					break
				}
			}
		}
		if index >= 0 {
			used[index] = true
			if ref == "" {
				ref = strconv.Itoa(index - len(m.service.Frames))
			}
		}
		options = append(options, editSessionOption{frame: frame, index: index, ref: ref})
	}
	return options
}

func sameFrameIdentity(a, b store.Frame) bool {
	if a.Start != b.Start || a.Project != b.Project || len(a.Tags) != len(b.Tags) {
		return false
	}
	if a.Stop == nil || b.Stop == nil {
		if a.Stop != b.Stop {
			return false
		}
	} else if *a.Stop != *b.Stop {
		return false
	}
	return sameStrings(a.Tags, b.Tags)
}

func (m dashboardModel) logEditableFrames() []editSessionOption {
	owned := agentOwnedFrameIDs(m.service)
	frames := m.logDisplayFrames()
	editable := make([]editSessionOption, 0, len(frames))
	for _, option := range frames {
		if option.frame.Stop != nil && option.index >= 0 && !owned[option.frame.ID] {
			editable = append(editable, option)
		}
	}
	return editable
}

func (m *dashboardModel) startLogEditSelection() {
	options := m.logEditableFrames()
	if len(options) == 0 {
		m.editNotice = "No completed editable sessions in this view."
		return
	}
	m.editNotice = ""
	m.logEditMode = true
	m.logEditCursor = max(0, min(m.logEditCursor, len(options)-1))
	m.logEditRef = options[m.logEditCursor].ref
	m.keepLogEditSelectionVisible()
}

func (m *dashboardModel) moveLogEditSelection(delta int) {
	options := m.logEditableFrames()
	if len(options) == 0 {
		m.logEditMode = false
		m.logEditRef = ""
		m.editNotice = "No completed editable sessions in this view."
		return
	}
	m.logEditCursor = max(0, min(len(options)-1, m.logEditCursor+delta))
	m.logEditRef = options[m.logEditCursor].ref
	m.keepLogEditSelectionVisible()
}

func (m *dashboardModel) keepLogEditSelectionVisible() {
	_, positions := m.logLayout()
	line, ok := positions[m.logEditRef]
	if !ok {
		return
	}
	if line < m.scroll {
		m.scroll = line
	} else if line >= m.scroll+m.viewportHeight() {
		m.scroll = line - m.viewportHeight() + 1
	}
	m.clampScroll()
}

func (m *dashboardModel) openLogEditForm() {
	options := m.logEditableFrames()
	if m.logEditCursor < 0 || m.logEditCursor >= len(options) {
		return
	}
	selected := options[m.logEditCursor]
	m.editForm = ptrSessionEditModel(newSessionEditModel(selected.frame, selected.ref, m.now))
	m.screen = tuiEdit
	m.editNotice = ""
}

func ptrSessionEditModel(model sessionEditModel) *sessionEditModel { return &model }

func (m dashboardModel) updateEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.editForm == nil {
		m.screen = tuiLog
		return m, nil
	}
	updated, _ := m.editForm.Update(msg)
	form := updated.(sessionEditModel)
	m.editForm = &form
	if form.canceled {
		m.editForm = nil
		m.screen = tuiLog
		return m, nil
	}
	if !form.confirmed {
		return m, nil
	}
	if !form.hasChanges() {
		m.editForm = nil
		m.screen = tuiLog
		m.logEditMode = true
		m.editNotice = "No changes made."
		m.keepLogEditSelectionVisible()
		return m, nil
	}
	proposal := form.proposal()
	updatedFrame, err := applyFrameEdit(m.service, form.ref, proposal)
	if err != nil {
		form.confirmed = false
		form.confirm = false
		form.errorText = err.Error()
		m.editForm = &form
		return m, nil
	}
	if proposal.MessageSet {
		updatedFrame, err = m.service.SetFrameMessage(form.ref, proposal.Message)
		if err != nil {
			form.confirmed = false
			form.confirm = false
			form.errorText = err.Error()
			m.editForm = &form
			return m, nil
		}
	}
	editedRef := form.ref
	m.editForm = nil
	m.screen = tuiLog
	m.logEditMode = true
	m.logEditRef = editedRef
	m.editNotice = "Session saved."
	if updatedFrame.ID != "" {
		m.logEditRef = updatedFrame.ID
	}
	options := m.logEditableFrames()
	matched := false
	for i, option := range options {
		if option.ref == m.logEditRef {
			m.logEditCursor = i
			matched = true
			break
		}
	}
	if !matched && len(options) > 0 {
		m.logEditCursor = max(0, min(m.logEditCursor, len(options)-1))
		m.logEditRef = options[m.logEditCursor].ref
	}
	m.keepLogEditSelectionVisible()
	return m, nil
}

func (m dashboardModel) reportView() string {
	frames := m.tuiFrames(true)
	options, _ := m.tuiOptions(true)
	summary := btReport.Build(frames, options)
	lines := m.reportLines(summary)
	toolbar := m.queryToolbar("REPORT", len(summary.Projects), "projects")
	content, position := m.viewport(lines)
	footer := m.queryFooter()
	footer += tuiMutedStyle.Render("  m messages " + visibilityLabel(m.reportMessagesVisible))
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
		if m.reportMessagesVisible {
			for _, tag := range project.Tags {
				seen := map[string]bool{}
				for _, frame := range m.tuiFrames(true) {
					if frame.Project != project.Name || frame.Message == "" || !containsString(frame.Tags, tag.Name) || seen[frame.Message] {
						continue
					}
					lines = append(lines, "      "+tuiMutedStyle.Render("↳ "+frame.Message))
					seen[frame.Message] = true
				}
			}
		}
		lines = append(lines, "")
	}
	return lines
}

func (m dashboardModel) aggregateView() string {
	summaries, dailyFrames := m.aggregateSummariesWithFrames()
	lines := m.aggregateSummaryLinesWithFrames(summaries, dailyFrames)
	days := 0
	for _, summary := range summaries {
		if len(summary.Projects) > 0 {
			days++
		}
	}
	toolbar := m.queryToolbar("AGGREGATE", days, "days")
	content, position := m.viewport(lines)
	footer := m.queryFooter()
	footer += tuiMutedStyle.Render("  m messages " + visibilityLabel(m.aggregateMessagesVisible))
	return m.header("Aggregate") + "\n\n" + toolbar + "\n\n" + content + "\n" + padBetween(footer, position, m.contentWidth())
}

func (m dashboardModel) aggregateLines() []string {
	summaries, dailyFrames := m.aggregateSummariesWithFrames()
	return m.aggregateSummaryLinesWithFrames(summaries, dailyFrames)
}

func (m dashboardModel) aggregateSummaries() []btReport.Summary {
	summaries, _ := m.aggregateSummariesWithFrames()
	return summaries
}

func (m dashboardModel) aggregateSummariesWithFrames() ([]btReport.Summary, [][]store.Frame) {
	options, err := m.tuiOptions(true)
	if err != nil {
		return nil, nil
	}
	frames := m.tuiFrames(true)
	return aggregateDailyWithFrames(frames, nil, m.now, options)
}

func (m dashboardModel) aggregateSummaryLines(summaries []btReport.Summary) []string {
	return m.aggregateSummaryLinesWithFrames(summaries, nil)
}

func (m dashboardModel) aggregateSummaryLinesWithFrames(summaries []btReport.Summary, dailyFrames [][]store.Frame) []string {
	width := m.contentWidth()
	lines := []string{}
	for dayIndex, summary := range summaries {
		if len(summary.Projects) == 0 {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		day := summary.Timespan.From
		dayDuration := formatDuration(int64(summary.Time))
		dayLabel := truncate(day.Format("Monday, January 2, 2006"), max(8, width-lipgloss.Width(dayDuration)-2))
		dayTitle := tuiBrandStyle.Render(dayLabel)
		lines = append(lines, padBetween(dayTitle, tuiTimeStyle.Bold(true).Render(dayDuration), width))
		var frames []store.Frame
		if dayIndex < len(dailyFrames) {
			frames = dailyFrames[dayIndex]
		}
		for projectIndex, project := range summary.Projects {
			nameWidth := min(32, max(10, width-20))
			projectLine := "  " + tuiTitleStyle.Foreground(tuiAccent).Render(fmt.Sprintf("%-*s", nameWidth, truncate(project.Name, nameWidth)))
			lines = append(lines, padBetween(projectLine, tuiTimeStyle.Render(formatDuration(int64(project.Time))), width))
			for _, tag := range project.Tags {
				tagWidth := min(40, max(10, width-20))
				tagLine := "    " + tuiTagStyle.Render("#"+truncate(tag.Name, tagWidth))
				lines = append(lines, padBetween(tagLine, tuiMutedStyle.Render(formatDuration(int64(tag.Time))), width))
				if m.aggregateMessagesVisible {
					seen := map[string]bool{}
					for _, frame := range frames {
						if frame.Project != project.Name || frame.Message == "" || !containsString(frame.Tags, tag.Name) || seen[frame.Message] {
							continue
						}
						message := "      " + tuiMutedStyle.Render(truncate("↳ "+frame.Message, max(1, width-6)))
						lines = append(lines, message)
						seen[frame.Message] = true
					}
				}
			}
			if projectIndex < len(summary.Projects)-1 {
				lines = append(lines, "")
			}
		}
	}
	if len(lines) == 0 {
		return []string{"", tuiMutedStyle.Render("  No tracked time matches this range and filter.")}
	}
	return lines
}

func visibilityLabel(visible bool) string {
	if visible {
		return "on"
	}
	return "off"
}
func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
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

func (m dashboardModel) queryFooter() string {
	if m.contentWidth() < 96 {
		return tuiHelp("←/→", "range", "/", "filter", "j/k", "scroll", "tab", "view", "b", "back")
	}
	return tuiHelp("←/→", "range", "/", "filter", "j/k", "scroll", "tab", "views", "a", "aggregate", "b", "back", "q", "quit")
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
	if m.screen == tuiAggregate {
		return len(m.aggregateLines())
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

type startConflictChoice string

const (
	startConflictReplace    startConflictChoice = "replace"
	startConflictConcurrent startConflictChoice = "concurrent"
)

type startConflictModel struct {
	timers    []store.ActiveTimer
	project   string
	tags      []string
	now       time.Time
	cursor    int
	selection startConflictChoice
	canceled  bool
	width     int
}

func (m startConflictModel) Init() tea.Cmd { return nil }

func (m startConflictModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.canceled = true
			return m, tea.Quit
		case "up", "k", "shift+tab":
			m.cursor = wrapIndex(m.cursor-1, 2)
		case "down", "j", "tab":
			m.cursor = wrapIndex(m.cursor+1, 2)
		case "r":
			m.selection = startConflictReplace
			return m, tea.Quit
		case "c":
			m.selection = startConflictConcurrent
			return m, tea.Quit
		case "enter":
			if m.cursor == 0 {
				m.selection = startConflictReplace
			} else {
				m.selection = startConflictConcurrent
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m startConflictModel) View() string {
	width := max(50, min(86, m.width-4))
	if m.width == 0 {
		width = 72
	}

	running := make([]string, 0, min(len(m.timers), 5)+1)
	for i, timer := range m.timers {
		if i == 5 {
			running = append(running, tuiMutedStyle.Render(fmt.Sprintf("  … and %d more", len(m.timers)-i)))
			break
		}
		elapsed := m.now.Sub(time.Unix(timer.Start, 0))
		if elapsed < 0 {
			elapsed = 0
		}
		name := tuiTitleStyle.Render(timer.Project)
		if len(timer.Tags) > 0 {
			name += "  " + tuiTagStyle.Render("#"+strings.Join(timer.Tags, "  #"))
		}
		running = append(running, padBetween("  "+name, tuiTimeStyle.Render(formatDuration(int64(elapsed/time.Second))), width-8))
	}

	newTimer := tuiTitleStyle.Foreground(tuiPrimary).Render(m.project)
	if len(m.tags) > 0 {
		newTimer += "  " + tuiTagStyle.Render("#"+strings.Join(m.tags, "  #"))
	}

	options := make([]string, 0, 2)
	replaceLabel := "Stop the current timer and replace it"
	if len(m.timers) > 1 {
		replaceLabel = fmt.Sprintf("Stop all %d timers and replace them", len(m.timers))
	}
	labels := []string{replaceLabel, "Start the new timer concurrently"}
	details := []string{
		"Finish active work, then start " + m.project,
		"Keep active work running alongside " + m.project,
	}
	for i := range labels {
		marker := "  "
		labelStyle := tuiTitleStyle
		if i == m.cursor {
			marker = tuiBrandStyle.Render("› ")
			labelStyle = tuiTitleStyle.Foreground(tuiPrimary)
		}
		options = append(options, marker+labelStyle.Render(labels[i])+"\n    "+tuiMutedStyle.Render(details[i]))
	}

	body := tuiMutedStyle.Render("BurrowTime is already tracking:") + "\n" +
		strings.Join(running, "\n") + "\n\n" +
		tuiMutedStyle.Render("You asked to start:") + "\n  " + newTimer + "\n\n" +
		strings.Join(options, "\n\n")
	footer := tuiHelp("↑/↓", "choose", "enter", "confirm", "r", "replace", "c", "concurrent", "esc", "cancel")
	return lipgloss.NewStyle().Padding(1, 2).Render(tuiBrandStyle.Render("◷  BURROWTIME") + "\n\n" + tuiPanel("TIMER ALREADY RUNNING", body, width) + "\n" + footer)
}

func runStartConflictPicker(timers []store.ActiveTimer, project string, tags []string, now time.Time) (startConflictChoice, error) {
	model := startConflictModel{
		timers:  append([]store.ActiveTimer(nil), timers...),
		project: project,
		tags:    append([]string(nil), tags...),
		now:     now,
		width:   76,
	}
	result, err := tea.NewProgram(model, tea.WithAltScreen()).Run()
	if err != nil {
		return "", err
	}
	final := result.(startConflictModel)
	if final.canceled || final.selection == "" {
		return "", errors.New("Aborted!")
	}
	return final.selection, nil
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
