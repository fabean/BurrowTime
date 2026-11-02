package cli

import (
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fabean/BurrowTime/internal/store"
	"github.com/fabean/BurrowTime/internal/watson"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type sessionWizardKind uint8

const (
	startWizard sessionWizardKind = iota
	addWizard
)

type sessionWizardStep uint8

const (
	wizardProject sessionWizardStep = iota
	wizardTags
	wizardStart
	wizardEnd
	wizardMessage
	wizardConfirm
)

type sessionWizardDefaults struct {
	Project string
	Tags    []string
	Start   string
	End     string
	Message string
}

type sessionWizardResult struct {
	Project string
	Tags    []string
	Start   *time.Time
	End     *time.Time
	Message string
}

type sessionWizardModel struct {
	kind           sessionWizardKind
	step           sessionWizardStep
	frames         []store.Frame
	timers         []store.ActiveTimer
	projects       []string
	projectTags    []string
	defaultTags    []string
	defaultTagList func(string) []string
	project        string
	tags           []string
	startText      string
	endText        string
	messageText    string
	input          []rune
	initialInput   string
	projectCursor  int
	tagCursor      int
	startTime      time.Time
	endTime        time.Time
	hasStart       bool
	hasEnd         bool
	now            time.Time
	nowFunc        func() time.Time
	width          int
	height         int
	errorText      string
	confirmed      bool
	canceled       bool
	minutePrecision bool
}

func recentProjects(s *watson.Service) []string {
	seen := map[string]bool{}
	projects := make([]string, 0, len(s.Frames)+len(s.RunningTimers()))
	for _, timer := range s.RunningTimers() {
		if timer.Project != "" && !seen[timer.Project] {
			seen[timer.Project] = true
			projects = append(projects, timer.Project)
		}
	}
	for i := len(s.Frames) - 1; i >= 0; i-- {
		project := s.Frames[i].Project
		if project != "" && !seen[project] {
			seen[project] = true
			projects = append(projects, project)
		}
	}
	remaining := make([]string, 0)
	for _, project := range s.Projects() {
		if !seen[project] {
			remaining = append(remaining, project)
		}
	}
	sort.Strings(remaining)
	return append(projects, remaining...)
}

func recentTagsForProject(frames []store.Frame, timers []store.ActiveTimer, project string) []string {
	seen := map[string]bool{}
	tags := make([]string, 0)
	for _, timer := range timers {
		if timer.Project == project {
			for _, tag := range timer.Tags {
				if tag != "" && !seen[tag] {
					seen[tag] = true
					tags = append(tags, tag)
				}
			}
		}
	}
	for i := len(frames) - 1; i >= 0; i-- {
		if frames[i].Project != project {
			continue
		}
		for _, tag := range frames[i].Tags {
			if tag != "" && !seen[tag] {
				seen[tag] = true
				tags = append(tags, tag)
			}
		}
	}
	return tags
}

func newSessionWizardModel(s *watson.Service, kind sessionWizardKind, defaults sessionWizardDefaults) sessionWizardModel {
	model := sessionWizardModel{
		kind:           kind,
		frames:         append([]store.Frame(nil), s.Frames...),
		timers:         append([]store.ActiveTimer(nil), s.RunningTimers()...),
		projects:       recentProjects(s),
		defaultTags:    append([]string(nil), s.Config.List("default_tags", defaults.Project)...),
		defaultTagList: func(project string) []string { return s.Config.List("default_tags", project) },
		project:        strings.TrimSpace(defaults.Project),
		tags:           append([]string(nil), defaults.Tags...),
		now:            s.Now(),
		nowFunc:        s.Now,
		minutePrecision: minutePrecisionEnabled(s, true),
		width:          80,
		height:         24,
		step:           wizardProject,
	}
	model.input = []rune(model.project)
	model.initialInput = model.project
	model.startText = defaults.Start
	model.endText = defaults.End
	model.messageText = defaults.Message
	model.now = model.currentTime()
	return model
}

func (m sessionWizardModel) currentTime() time.Time {
	now := m.nowFunc()
	if m.minutePrecision {
		return floorMinute(now)
	}
	return now
}

func (m sessionWizardModel) filteredProjects() []string {
	query := strings.ToLower(strings.TrimSpace(string(m.input)))
	if query == "" {
		return append([]string(nil), m.projects...)
	}
	filtered := make([]string, 0, len(m.projects))
	for _, project := range m.projects {
		if strings.Contains(strings.ToLower(project), query) {
			filtered = append(filtered, project)
		}
	}
	return filtered
}

func (m sessionWizardModel) filteredTags() []string {
	query := strings.ToLower(strings.TrimSpace(string(m.input)))
	if query == "" {
		return append([]string(nil), m.projectTags...)
	}
	filtered := make([]string, 0, len(m.projectTags))
	for _, tag := range m.projectTags {
		if strings.Contains(strings.ToLower(tag), query) {
			filtered = append(filtered, tag)
		}
	}
	return filtered
}

func (m sessionWizardModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			m.canceled = true
			return m, tea.Quit
		}
		if m.step == wizardConfirm {
			switch key {
			case "enter", "y":
				m.confirmed = true
				return m, tea.Quit
			case "n":
				m.canceled = true
				return m, tea.Quit
			case "esc", "backspace":
				m.step = m.previousStep()
				m.loadStepInput()
				m.errorText = ""
			}
			return m, nil
		}
		if key == "esc" {
			if m.step == wizardProject {
				m.canceled = true
				return m, tea.Quit
			}
			m.step = m.previousStep()
			m.loadStepInput()
			m.errorText = ""
			return m, nil
		}
		if m.step == wizardProject || m.step == wizardTags {
			switch key {
			case "up", "shift+tab":
				m.moveSuggestion(-1)
				return m, nil
			case "down":
				m.moveSuggestion(1)
				return m, nil
			case "ctrl+n":
				if m.step == wizardProject {
					m.acceptProject(strings.TrimSpace(string(m.input)))
					return m, nil
				}
			case "tab":
				if m.step == wizardTags {
					m.acceptSuggestedTag()
					return m, nil
				}
			}
		}
		if key == "enter" {
			m.acceptField()
			return m, nil
		}
		m.editInput(msg)
	}
	return m, nil
}

func (m *sessionWizardModel) moveSuggestion(delta int) {
	count := 0
	if m.step == wizardProject {
		count = len(m.filteredProjects())
		m.projectCursor = wrapIndex(m.projectCursor+delta, count)
	} else {
		count = len(m.filteredTags())
		m.tagCursor = wrapIndex(m.tagCursor+delta, count)
	}
}

func (m *sessionWizardModel) editInput(msg tea.KeyMsg) {
	if msg.Type == tea.KeyCtrlU {
		m.input = nil
		m.errorText = ""
		m.resetSuggestionCursors()
		return
	}
	switch msg.Type {
	case tea.KeyBackspace, tea.KeyDelete:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	case tea.KeySpace:
		m.input = append(m.input, ' ')
	case tea.KeyRunes:
		m.input = append(m.input, msg.Runes...)
	}
	m.errorText = ""
	m.resetSuggestionCursors()
}

func (m *sessionWizardModel) resetSuggestionCursors() {
	m.projectCursor, m.tagCursor = 0, 0
}

func (m *sessionWizardModel) acceptField() {
	value := strings.TrimSpace(string(m.input))
	switch m.step {
	case wizardProject:
		if value == m.initialInput && value != "" {
			m.acceptProject(value)
			return
		}
		matches := m.filteredProjects()
		if value == "" {
			if len(matches) == 0 {
				m.errorText = "Enter a project name, or press Ctrl+C to cancel."
				return
			}
			m.acceptProject(matches[min(m.projectCursor, len(matches)-1)])
			return
		}
		for _, project := range matches {
			if strings.EqualFold(value, project) {
				m.acceptProject(project)
				return
			}
		}
		if len(matches) > 0 {
			m.acceptProject(matches[min(m.projectCursor, len(matches)-1)])
			return
		}
		m.acceptProject(value)
	case wizardTags:
		if value != "" {
			m.tags = appendUnique(m.tags, splitWizardTags(value)...)
		}
		m.step = wizardStart
		m.now = m.currentTime()
		m.input = []rune(m.startText)
		m.initialInput = m.startText
		m.errorText = ""
	case wizardStart:
		if value == "" && m.kind == startWizard {
			m.startTime = m.currentTime()
			m.now = m.startTime
			m.hasStart = true
			m.startText = ""
			m.step = wizardMessage
			m.input = []rune(m.messageText)
			m.initialInput = m.messageText
			m.errorText = ""
			return
		}
		if value == "" {
			m.errorText = "Enter a start time."
			return
		}
		parsed, err := parseTime(value, m.nowFunc())
		if err != nil {
			m.errorText = err.Error()
			return
		}
		if m.kind == startWizard && parsed.After(m.nowFunc()) {
			m.errorText = "A timer cannot start in the future."
			return
		}
		m.startText, m.startTime, m.hasStart = value, parsed, true
		if m.kind == addWizard {
			m.step = wizardEnd
			m.input = []rune(m.endText)
			m.initialInput = m.endText
		} else {
			m.step = wizardMessage
			m.input = []rune(m.messageText)
			m.initialInput = m.messageText
		}
		m.errorText = ""
	case wizardEnd:
		if value == "" {
			m.errorText = "Enter an end time."
			return
		}
		parsed, err := parseTime(value, m.startTime)
		if err != nil {
			m.errorText = err.Error()
			return
		}
		if !parsed.After(m.startTime) {
			m.errorText = "End time must be after start time; add an explicit end date for overnight sessions."
			return
		}
		m.endText, m.endTime, m.hasEnd = value, parsed, true
		m.step = wizardMessage
		m.input = []rune(m.messageText)
		m.initialInput = m.messageText
		m.errorText = ""
	case wizardMessage:
		m.messageText = value
		m.step = wizardConfirm
		m.input = nil
		m.errorText = ""
	}
}

func (m *sessionWizardModel) acceptProject(project string) {
	project = strings.TrimSpace(project)
	if project == "" {
		m.errorText = "Enter a project name, or press Ctrl+C to cancel."
		return
	}
	m.project = project
	m.projectTags = recentTagsForProject(m.frames, m.timers, project)
	m.defaultTags = m.defaultTagList(project)
	m.step = wizardTags
	m.input = []rune(strings.Join(m.tags, ", "))
	m.initialInput = string(m.input)
	m.errorText = ""
}

func (m *sessionWizardModel) acceptSuggestedTag() {
	tags := m.filteredTags()
	if len(tags) == 0 {
		return
	}
	index := min(m.tagCursor, len(tags)-1)
	m.tags = appendUnique(m.tags, tags[index])
	m.input = nil
	m.initialInput = ""
	m.errorText = ""
}

func splitWizardTags(value string) []string {
	parts := strings.Split(value, ",")
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		if tag := strings.TrimSpace(part); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

func appendUnique(existing []string, values ...string) []string {
	seen := make(map[string]bool, len(existing)+len(values))
	result := make([]string, 0, len(existing)+len(values))
	for _, value := range append(append([]string(nil), existing...), values...) {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func (m sessionWizardModel) Init() tea.Cmd { return nil }

func (m sessionWizardModel) previousStep() sessionWizardStep {
	switch m.step {
	case wizardTags:
		return wizardProject
	case wizardStart:
		return wizardTags
	case wizardEnd:
		return wizardStart
	case wizardMessage:
		if m.kind == addWizard {
			return wizardEnd
		}
		return wizardStart
	case wizardConfirm:
		return wizardMessage
	default:
		return wizardProject
	}
}

func (m *sessionWizardModel) loadStepInput() {
	switch m.step {
	case wizardProject:
		m.input = []rune(m.project)
	case wizardTags:
		m.input = []rune(strings.Join(m.tags, ", "))
	case wizardStart:
		m.input = []rune(m.startText)
	case wizardEnd:
		m.input = []rune(m.endText)
	case wizardMessage:
		m.input = []rune(m.messageText)
	}
	m.initialInput = string(m.input)
}

func (m sessionWizardModel) effectiveTags() []string {
	combined := append(append([]string(nil), m.tags...), m.defaultTags...)
	if m.kind == startWizard {
		return appendUnique(nil, combined...)
	}
	return combined
}

func (m sessionWizardModel) result() sessionWizardResult {
	result := sessionWizardResult{Project: m.project, Tags: append([]string(nil), m.tags...), Message: m.messageText}
	if m.hasStart && !(m.kind == startWizard && m.startText == "") {
		start := m.startTime
		result.Start = &start
		if m.kind == startWizard && m.startText == "" {
			result.Start = nil
		}
	}
	if m.kind == addWizard && m.hasEnd {
		end := m.endTime
		result.End = &end
	}
	return result
}

func (m sessionWizardModel) View() string {
	width := max(54, min(88, m.width-4))
	if m.width < 1 {
		width = 72
	}
	var title string
	if m.kind == startWizard {
		title = "START A SESSION"
	} else {
		title = "ADD A COMPLETED SESSION"
	}
	var body string
	var help string
	switch m.step {
	case wizardProject:
		body, help = m.projectView(width)
	case wizardTags:
		body, help = m.tagsView(width)
	case wizardStart:
		body = m.timeView("Start time", m.startText, m.kind == startWizard, width)
		help = tuiHelp("enter", "continue", "ctrl+u", "clear", "esc", "back", "ctrl+c", "cancel")
	case wizardEnd:
		body = m.timeView("End time", m.endText, false, width)
		help = tuiHelp("enter", "continue", "ctrl+u", "clear", "esc", "back", "ctrl+c", "cancel")
	case wizardMessage:
		value := strings.TrimSpace(string(m.input))
		if value == "" {
			value = "Optional message; press Enter to skip"
		}
		body = tuiBrandStyle.Render("Message") + "\n" + tuiTagStyle.Render(truncate(value, width-8)) + " ▏"
		help = tuiHelp("type", "message", "enter", "continue/skip", "esc", "back", "ctrl+c", "cancel")
	case wizardConfirm:
		body = m.confirmView(width)
		help = tuiHelp("enter/y", "confirm", "esc", "edit previous", "ctrl+c", "cancel")
	}
	return tuiBrandStyle.Render("◷  BURROWTIME") + "\n\n" + tuiPanel(title, body, width) + "\n" + help
}

func (m sessionWizardModel) projectView(width int) (string, string) {
	query := strings.TrimSpace(string(m.input))
	projects := m.filteredProjects()
	rows := max(3, min(8, m.height-17))
	start := max(0, m.projectCursor-rows+1)
	var lines []string
	for i := start; i < min(len(projects), start+rows); i++ {
		marker, style := "  ", tuiTitleStyle
		if i == m.projectCursor {
			marker, style = tuiBrandStyle.Render("› "), tuiTitleStyle.Foreground(tuiPrimary).Background(tuiSurface).Bold(true)
		}
		lines = append(lines, style.Width(width-8).Render(marker+truncate(projects[i], width-10)))
	}
	if len(projects) == 0 {
		lines = append(lines, tuiMutedStyle.Render("No matching projects. Enter the new name and press Enter."))
	}
	if query == "" {
		query = "Type to search or enter a new project"
	}
	body := tuiBrandStyle.Render("Project") + "\n" + tuiTagStyle.Render(truncate(query, width-8)) + " ▏\n\n" + strings.Join(lines, "\n")
	if m.errorText != "" {
		body += "\n\n" + tuiTitleStyle.Foreground(tuiWarning).Bold(true).Render(m.errorText)
	}
	return body, tuiHelp("type", "search", "↑/↓", "choose", "enter", "select", "ctrl+n", "new project", "ctrl+c", "cancel")
}

func (m sessionWizardModel) tagsView(width int) (string, string) {
	var lines []string
	for i, tag := range m.filteredTags() {
		marker, style := "  ", tuiTitleStyle
		if i == m.tagCursor {
			marker, style = tuiBrandStyle.Render("› "), tuiTitleStyle.Foreground(tuiPrimary).Background(tuiSurface).Bold(true)
		}
		lines = append(lines, style.Width(width-8).Render(marker+truncate(tag, width-10)))
	}
	if len(lines) == 0 {
		lines = append(lines, tuiMutedStyle.Render("No matching tags. Enter new tags separated by commas."))
	}
	entry := strings.TrimSpace(string(m.input))
	if entry == "" {
		entry = "Type tags separated by commas, or press Enter to skip"
	}
	body := tuiBrandStyle.Render("Tags for "+m.project) + "\n" + tuiTagStyle.Render(truncate(entry, width-8)) + " ▏\n"
	if len(m.tags) > 0 {
		body += "\nSelected: " + tuiTagStyle.Render(strings.Join(m.tags, ", ")) + "\n"
	}
	body += "\n" + strings.Join(lines, "\n")
	if m.errorText != "" {
		body += "\n\n" + tuiTitleStyle.Foreground(tuiWarning).Bold(true).Render(m.errorText)
	}
	return body, tuiHelp("type", "new tags", "↑/↓", "browse", "tab", "add suggestion", "enter", "continue/skip", "esc", "back")
}

func (m sessionWizardModel) timeView(label, current string, allowNow bool, width int) string {
	value := string(m.input)
	if value == "" && allowNow {
		value = "Now"
	}
	if value == "" {
		value = "Enter date and time"
	}
	body := tuiBrandStyle.Render(label) + "\n" + tuiTagStyle.Render(truncate(value, width-8)) + " ▏\n\n" + tuiMutedStyle.Render("Use the normal CLI date/time formats, such as 09:30 or 2026-10-26 09:30.\nA time-only end value uses the start date; add an end date for overnight work.")
	if m.errorText != "" {
		body += "\n\n" + tuiTitleStyle.Foreground(tuiWarning).Bold(true).Render(m.errorText)
	}
	return body
}

func (m sessionWizardModel) confirmView(width int) string {
	loc := watsonDisplayLocation(m.now)
	start := m.startTime.In(loc).Format("2006-01-02 15:04:05")
	if m.kind == startWizard && m.startText == "" {
		start = m.now.In(loc).Format("2006-01-02 15:04:05") + " (Now)"
	}
	lines := []string{
		padBetween("Project:", tuiTitleStyle.Render(m.project), width-8),
		padBetween("Tags:", tuiTagStyle.Render(orDash(strings.Join(m.effectiveTags(), ", "))), width-8),
		padBetween("Start:", tuiTimeStyle.Render(start), width-8),
	}
	if m.kind == addWizard {
		lines = append(lines,
			padBetween("End:", tuiTimeStyle.Render(m.endTime.In(loc).Format("2006-01-02 15:04:05")), width-8),
			padBetween("Duration:", tuiTimeStyle.Render(formatDuration(int64(m.endTime.Sub(m.startTime)/time.Second))), width-8),
		)
	}
	if m.messageText != "" {
		lines = append(lines, padBetween("Message:", tuiTitleStyle.Render(m.messageText), width-8))
	}
	if m.errorText != "" {
		lines = append(lines, "", tuiTitleStyle.Foreground(tuiWarning).Bold(true).Render(m.errorText))
	}
	return strings.Join(lines, "\n")
}

func orDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

func runSessionWizard(cmd *cobra.Command, s *watson.Service, kind sessionWizardKind, defaults sessionWizardDefaults) (sessionWizardResult, error) {
	in, inOK := cmd.InOrStdin().(*os.File)
	out, outOK := cmd.OutOrStdout().(*os.File)
	if !inOK || !outOK || !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return sessionWizardResult{}, errors.New("Interactive mode requires a terminal for both input and output.")
	}
	model := newSessionWizardModel(s, kind, defaults)
	result, err := tea.NewProgram(model, tea.WithInput(in), tea.WithOutput(out), tea.WithAltScreen()).Run()
	if err != nil {
		return sessionWizardResult{}, err
	}
	final := result.(sessionWizardModel)
	if final.canceled || !final.confirmed {
		return sessionWizardResult{}, errors.New("Aborted!")
	}
	return final.result(), nil
}
