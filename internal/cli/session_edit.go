package cli

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fabean/BurrowTime/internal/store"
	"github.com/fabean/BurrowTime/internal/watson"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type editSessionOption struct {
	frame store.Frame
	index int
	ref   string
}

func agentOwnedFrameIDs(s *watson.Service) map[string]bool {
	ids := map[string]bool{}
	for _, session := range s.AgentSessions {
		for _, id := range session.FrameIDs {
			ids[id] = true
		}
	}
	return ids
}

func editableSessions(s *watson.Service) []editSessionOption {
	owned := agentOwnedFrameIDs(s)
	options := make([]editSessionOption, 0, len(s.Frames))
	for i, frame := range s.Frames {
		if frame.Stop == nil || frame.ID != "" && owned[frame.ID] {
			continue
		}
		ref := frame.ID
		if ref == "" {
			ref = strconv.Itoa(i - len(s.Frames))
		}
		options = append(options, editSessionOption{frame: frame, index: i, ref: ref})
	}
	sort.SliceStable(options, func(i, j int) bool { return options[i].frame.Start > options[j].frame.Start })
	return options
}

type sessionPickerModel struct {
	options   []editSessionOption
	filtered  []editSessionOption
	query     []rune
	cursor    int
	searching bool
	width     int
	height    int
	now       time.Time
	selected  string
	canceled  bool
}

func newSessionPickerModel(s *watson.Service, preselect string) sessionPickerModel {
	m := sessionPickerModel{
		options: editableSessions(s),
		width:   88,
		height:  24,
		now:     s.Now(),
	}
	if preselect != "" {
		for i, option := range m.options {
			if option.ref == preselect || strings.HasPrefix(option.frame.ID, preselect) {
				m.cursor = i
				break
			}
		}
	}
	m.updateFiltered()
	return m
}

func sessionEditLocation(now time.Time) *time.Location {
	if tz := os.Getenv("TZ"); tz != "" {
		if loaded, err := time.LoadLocation(tz); err == nil {
			return loaded
		}
	}
	return now.Location()
}

func (m *sessionPickerModel) updateFiltered() {
	query := strings.ToLower(strings.TrimSpace(string(m.query)))
	m.filtered = m.filtered[:0]
	for _, option := range m.options {
		frame := option.frame
		haystack := strings.ToLower(strings.Join([]string{
			frame.ID,
			frame.Project,
			strings.Join(frame.Tags, " "),
			time.Unix(frame.Start, 0).In(sessionEditLocation(m.now)).Format("2006-01-02 15:04"),
			shortID(frame.ID),
		}, " "))
		if query == "" || strings.Contains(haystack, query) {
			m.filtered = append(m.filtered, option)
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
}

func (m sessionPickerModel) Init() tea.Cmd { return nil }

func (m sessionPickerModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "esc" {
			m.canceled = true
			return m, tea.Quit
		}
		if m.searching {
			switch msg.Type {
			case tea.KeyEnter:
				m.searching = false
			case tea.KeyBackspace, tea.KeyDelete:
				if len(m.query) > 0 {
					m.query = m.query[:len(m.query)-1]
				}
			case tea.KeyCtrlU:
				m.query = nil
			case tea.KeySpace:
				m.query = append(m.query, ' ')
			case tea.KeyRunes:
				m.query = append(m.query, msg.Runes...)
			}
			m.cursor = 0
			m.updateFiltered()
			return m, nil
		}
		switch msg.String() {
		case "up", "k":
			m.cursor = wrapIndex(m.cursor-1, len(m.filtered))
		case "down", "j":
			m.cursor = wrapIndex(m.cursor+1, len(m.filtered))
		case "enter":
			if len(m.filtered) == 0 {
				return m, nil
			}
			m.selected = m.filtered[m.cursor].ref
			return m, tea.Quit
		case "/":
			m.searching = true
		}
	}
	return m, nil
}

func (m sessionPickerModel) View() string {
	width := max(54, min(100, m.width-4))
	rows := max(3, min(12, m.height-12))
	start := max(0, m.cursor-rows+1)
	end := min(len(m.filtered), start+rows)
	var lines []string
	loc := sessionEditLocation(m.now)
	for i := start; i < end; i++ {
		option := m.filtered[i]
		frame := option.frame
		begin := time.Unix(frame.Start, 0).In(loc)
		finish := time.Unix(*frame.Stop, 0).In(loc)
		duration := formatDuration(*frame.Stop - frame.Start)
		id := shortID(frame.ID)
		if frame.ID == "" {
			id = fmt.Sprintf("#%d", option.index+1)
		}
		tags := ""
		if len(frame.Tags) > 0 {
			tags = "  #" + strings.Join(frame.Tags, " #")
		}
		label := fmt.Sprintf("%-7s  %s  %s → %s  %s%s", id, begin.Format("2006-01-02"), begin.Format("15:04"), finish.Format("15:04"), frame.Project, tags)
		line := "  " + truncate(label, max(1, width-23)) + "  " + tuiTimeStyle.Render(duration)
		if i == m.cursor {
			line = lipgloss.NewStyle().Foreground(tuiPrimary).Background(tuiSurface).Bold(true).Width(width - 4).Render("› " + truncate(label, max(1, width-23)) + "  " + duration)
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		lines = append(lines, tuiMutedStyle.Render("No completed editable sessions match. Press Esc to cancel."))
	}
	query := strings.TrimSpace(string(m.query))
	if query == "" && !m.searching {
		query = "Type to search project, tag, ID, or date"
	} else if m.searching {
		query = "/ " + query
	}
	body := tuiTagStyle.Render(truncate(query, width-8)) + " ▏\n\n" + strings.Join(lines, "\n")
	help := tuiHelp("↑/↓ or j/k", "select", "/", "search", "enter", "edit", "esc", "cancel")
	if m.searching {
		help = tuiHelp("type", "search", "enter", "finish search", "ctrl+u", "clear", "esc", "cancel")
	}
	return tuiBrandStyle.Render("◷  BURROWTIME") + "\n\n" + tuiPanel("SELECT SESSION TO EDIT", body, width) + "\n" + help
}

type sessionEditField uint8

const (
	editProject sessionEditField = iota
	editTags
	editStart
	editStop
	editMessage
	editSave
)

type sessionEditModel struct {
	frame     store.Frame
	ref       string
	project   string
	tags      []string
	start     time.Time
	stop      time.Time
	startText string
	stopText  string
	message   string
	field     sessionEditField
	input     []rune
	editing   bool
	confirm   bool
	confirmed bool
	canceled  bool
	errorText string
	now       time.Time
	loc       *time.Location
	width     int
}

func newSessionEditModel(frame store.Frame, ref string, now time.Time) sessionEditModel {
	loc := sessionEditLocation(now)
	start := time.Unix(frame.Start, 0).In(loc)
	stop := time.Unix(*frame.Stop, 0).In(loc)
	return sessionEditModel{
		frame: frame, ref: ref, project: frame.Project, tags: append([]string(nil), frame.Tags...), message: frame.Message,
		start: start, stop: stop,
		startText: start.Format("2006-01-02 15:04:05"), stopText: stop.Format("2006-01-02 15:04:05"),
		now: now, loc: loc, width: 80,
	}
}

func (m sessionEditModel) Init() tea.Cmd { return nil }

func (m sessionEditModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "esc" && !m.editing && !m.confirm {
			m.canceled = true
			return m, tea.Quit
		}
		if m.confirm {
			switch msg.String() {
			case "enter", "y":
				m.confirmed = true
				return m, tea.Quit
			case "n", "esc":
				m.confirm = false
			}
			return m, nil
		}
		if m.editing {
			m.updateValue(msg)
			return m, nil
		}
		switch msg.String() {
		case "esc":
			m.canceled = true
			return m, tea.Quit
		case "up", "k", "shift+tab":
			m.field = sessionEditField(wrapIndex(int(m.field)-1, 6))
		case "down", "j", "tab":
			m.field = sessionEditField(wrapIndex(int(m.field)+1, 6))
		case "enter":
			if m.field == editSave {
				m.beginConfirmation()
				return m, nil
			}
			m.beginValueEdit()
		}
	}
	return m, nil
}

func (m *sessionEditModel) beginConfirmation() {
	switch {
	case strings.TrimSpace(m.project) == "":
		m.errorText = "Project cannot be empty."
	case !m.stop.After(m.start):
		m.errorText = "Session duration must be greater than zero; end time must be after start time."
	case m.start.After(m.now):
		m.errorText = "Start time cannot be in the future."
	case m.stop.After(m.now):
		m.errorText = "Stop time cannot be in the future."
	default:
		m.errorText = ""
		m.confirm = true
	}
}

func (m sessionEditModel) changeSummary() []string {
	var changes []string
	if m.project != m.frame.Project {
		changes = append(changes, fmt.Sprintf("Project: %s → %s", m.frame.Project, m.project))
	}
	if !sameStrings(m.tags, m.frame.Tags) {
		changes = append(changes, fmt.Sprintf("Tags: %s → %s", strings.Join(m.frame.Tags, ", "), strings.Join(m.tags, ", ")))
	}
	originalStart := time.Unix(m.frame.Start, 0).In(m.loc)
	originalStop := time.Unix(*m.frame.Stop, 0).In(m.loc)
	if !m.start.Equal(originalStart) {
		changes = append(changes, fmt.Sprintf("Start: %s → %s", originalStart.Format("2006-01-02 15:04:05"), m.start.In(m.loc).Format("2006-01-02 15:04:05")))
	}
	if !m.stop.Equal(originalStop) {
		changes = append(changes, fmt.Sprintf("End: %s → %s", originalStop.Format("2006-01-02 15:04:05"), m.stop.In(m.loc).Format("2006-01-02 15:04:05")))
	}
	if m.message != m.frame.Message {
		changes = append(changes, fmt.Sprintf("Message: %s → %s", orDash(m.frame.Message), orDash(m.message)))
	}
	if len(changes) == 0 {
		changes = append(changes, "No field values changed.")
	}
	return changes
}

func (m *sessionEditModel) beginValueEdit() {
	m.editing = true
	m.errorText = ""
	switch m.field {
	case editProject:
		m.input = []rune(m.project)
	case editTags:
		m.input = []rune(strings.Join(m.tags, ", "))
	case editStart:
		m.input = []rune(m.startText)
	case editStop:
		m.input = []rune(m.stopText)
	case editMessage:
		m.input = []rune(m.message)
	}
}

func (m *sessionEditModel) updateValue(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyEnter:
		value := strings.TrimSpace(string(m.input))
		switch m.field {
		case editProject:
			if value == "" {
				m.errorText = "Project cannot be empty."
				return
			}
			m.project = value
		case editTags:
			m.tags = splitEditTags(value)
		case editStart:
			parsed, err := parseTime(value, m.start)
			if err != nil {
				m.errorText = fmt.Sprintf("Invalid start time %q. Try 14:30 or 2026-10-26 14:30:00: %v", value, err)
				return
			}
			m.start = parsed
			m.startText = value
		case editStop:
			parsed, err := parseTime(value, m.stop)
			if err != nil {
				m.errorText = fmt.Sprintf("Invalid end time %q. Try 14:30 or 2026-10-26 14:30:00: %v", value, err)
				return
			}
			m.stop = parsed
			m.stopText = value
		case editMessage:
			m.message = value
		}
		m.editing = false
		m.errorText = ""
	case tea.KeyEsc:
		m.editing = false
		m.input = nil
		m.errorText = ""
	case tea.KeyCtrlU:
		m.input = nil
		m.errorText = ""
	case tea.KeyBackspace, tea.KeyDelete:
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	case tea.KeySpace:
		m.input = append(m.input, ' ')
	case tea.KeyRunes:
		m.input = append(m.input, msg.Runes...)
	}
}

func splitEditTags(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return appendUniqueTags(strings.Split(value, ","))
}

func appendUniqueTags(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			result = append(result, value)
			seen[value] = true
		}
	}
	return result
}

func (m sessionEditModel) proposal() parsedEdit {
	return parsedEdit{Project: m.project, Tags: append([]string(nil), m.tags...), Start: m.start, Stop: m.stop, Message: m.message, MessageSet: m.message != m.frame.Message}
}

func (m sessionEditModel) hasChanges() bool {
	return m.project != m.frame.Project || !sameStrings(m.tags, m.frame.Tags) ||
		!m.start.Equal(time.Unix(m.frame.Start, 0).In(m.loc)) ||
		!m.stop.Equal(time.Unix(*m.frame.Stop, 0).In(m.loc)) || m.message != m.frame.Message
}

func (m sessionEditModel) View() string {
	width := max(54, min(88, m.width-4))
	fields := []struct {
		name  string
		value string
	}{
		{"Project", m.project},
		{"Tags", strings.Join(m.tags, ", ")},
		{"Start", m.start.In(m.loc).Format("2006-01-02 15:04:05")},
		{"End", m.stop.In(m.loc).Format("2006-01-02 15:04:05")},
		{"Message", m.message},
	}
	if m.editing {
		fields[m.field].value = string(m.input)
	}
	var lines []string
	for i, field := range fields {
		marker, style := "  ", tuiTitleStyle
		if int(m.field) == i {
			marker, style = tuiBrandStyle.Render("› "), tuiTitleStyle.Foreground(tuiPrimary).Bold(true)
		}
		lines = append(lines, marker+style.Render(padBetween(field.name+":", truncate(field.value, max(1, width-22)), width-10)))
	}
	duration := "—"
	if m.stop.After(m.start) {
		duration = formatDuration(int64(m.stop.Sub(m.start) / time.Second))
	}
	lines = append(lines, "", "    "+tuiMutedStyle.Render("Duration:")+" "+tuiTimeStyle.Render(duration))
	if m.field == editSave {
		lines = append(lines, tuiBrandStyle.Render("› ")+tuiTitleStyle.Foreground(tuiPrimary).Render("Save Changes"))
	} else {
		lines = append(lines, "  "+tuiMutedStyle.Render("Save Changes"))
	}
	if m.confirm {
		lines = append(lines, "", tuiTitleStyle.Render("Changes:"))
		for _, change := range m.changeSummary() {
			lines = append(lines, "  "+tuiMutedStyle.Render(truncate(change, width-10)))
		}
		lines = append(lines, "", tuiTitleStyle.Render("Save these changes?"))
	}
	if m.errorText != "" {
		lines = append(lines, "", tuiTitleStyle.Foreground(tuiWarning).Bold(true).Render(m.errorText))
	}
	help := tuiHelp("↑/↓", "field", "enter", "edit/save", "tab", "next", "esc", "cancel")
	if m.editing {
		help = tuiHelp("type", "edit value", "ctrl+u", "clear", "enter", "apply", "esc", "keep current")
	}
	if m.confirm {
		help = tuiHelp("enter/y", "confirm", "n/esc", "review", "ctrl+c", "cancel")
	}
	return tuiBrandStyle.Render("◷  BURROWTIME  /  Edit Session") + "\n\n" + tuiPanel("EDIT SESSION", strings.Join(lines, "\n"), width) + "\n" + help
}

func runSessionPicker(cmd *cobra.Command, s *watson.Service, preselect string) (string, error) {
	in, inOK := cmd.InOrStdin().(*os.File)
	out, outOK := cmd.OutOrStdout().(*os.File)
	if !inOK || !outOK || !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return "", errors.New("Interactive editing requires a terminal for both input and output.")
	}
	model := newSessionPickerModel(s, preselect)
	result, err := tea.NewProgram(model, tea.WithInput(in), tea.WithOutput(out), tea.WithAltScreen()).Run()
	if err != nil {
		return "", err
	}
	final := result.(sessionPickerModel)
	if final.canceled {
		return "", nil
	}
	if final.selected == "" {
		if len(final.options) == 0 {
			return "", errors.New("No completed editable sessions found.")
		}
		return "", errors.New("No session selected.")
	}
	return final.selected, nil
}

func runSessionEditForm(cmd *cobra.Command, model sessionEditModel) (sessionEditModel, error) {
	in, inOK := cmd.InOrStdin().(*os.File)
	out, outOK := cmd.OutOrStdout().(*os.File)
	if !inOK || !outOK || !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		return model, errors.New("Interactive editing requires a terminal for both input and output.")
	}
	result, err := tea.NewProgram(model, tea.WithInput(in), tea.WithOutput(out), tea.WithAltScreen()).Run()
	if err != nil {
		return model, err
	}
	return result.(sessionEditModel), nil
}

func sessionEditIsAgentOwned(s *watson.Service, frame store.Frame) bool {
	return frame.ID != "" && agentOwnedFrameIDs(s)[frame.ID]
}
