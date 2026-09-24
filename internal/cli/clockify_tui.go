package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/fabean/BurrowTime/internal/integrations"
)

type clockifyScreen int

const (
	clockifyPicker clockifyScreen = iota
	clockifyReview
	clockifyEditor
	clockifyConfirm
)

type clockifyModel struct {
	provider                                      string
	projects                                      []integrations.Project
	queue                                         []integrations.Receipt
	screen                                        clockifyScreen
	width, height, cursor, entryCursor            int
	local, selected, query, field, input, message string
	searching, pickingForEntry, dryRun, done      bool
}

func newClockifyModel(projects []integrations.Project) clockifyModel {
	return clockifyModel{provider: "Clockify", projects: append([]integrations.Project(nil), projects...), width: 90, height: 28}
}
func (m clockifyModel) Init() tea.Cmd { return nil }

func (m clockifyModel) filtered() []integrations.Project {
	projects := []integrations.Project{}
	for _, p := range m.projects {
		if m.query == "" || strings.Contains(projectWords(p.Name+" "+p.ClientName), projectWords(m.query)) || strings.Contains(strings.ToLower(p.ID), strings.ToLower(m.query)) {
			projects = append(projects, p)
		}
	}
	sort.SliceStable(projects, func(i, j int) bool {
		a, b := projectScore(m.local, projects[i].Name), projectScore(m.local, projects[j].Name)
		if a == b {
			return projects[i].Name+" "+projects[i].ClientLabel()+" "+projects[i].ID < projects[j].Name+" "+projects[j].ClientLabel()+" "+projects[j].ID
		}
		return a > b
	})
	return projects
}

func (m clockifyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		key := msg.String()
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.searching || m.field != "" {
			if key == "esc" {
				m.searching = false
				m.field = ""
				m.message = ""
				return m, nil
			}
			if key == "enter" {
				if m.searching {
					m.searching = false
				} else {
					m.applyEdit()
				}
				return m, nil
			}
			value := &m.input
			if m.searching {
				value = &m.query
			}
			switch msg.Type {
			case tea.KeyBackspace, tea.KeyDelete:
				r := []rune(*value)
				if len(r) > 0 {
					*value = string(r[:len(r)-1])
				}
			case tea.KeyCtrlU:
				*value = ""
			case tea.KeySpace:
				*value += " "
			case tea.KeyRunes:
				if len([]rune(*value))+len(msg.Runes) <= 3000 {
					*value += string(msg.Runes)
				}
			}
			if m.searching {
				m.cursor = 0
			}
			return m, nil
		}
		if key == "q" {
			return m, tea.Quit
		}
		switch m.screen {
		case clockifyPicker:
			projects := m.filtered()
			switch key {
			case "up", "k":
				m.cursor = wrapIndex(m.cursor-1, len(projects))
			case "down", "j":
				m.cursor = wrapIndex(m.cursor+1, len(projects))
			case "pgdown":
				m.cursor = min(max(0, len(projects)-1), m.cursor+m.listRows())
			case "pgup":
				m.cursor = max(0, m.cursor-m.listRows())
			case "/":
				m.searching = true
			case "esc":
				if m.query != "" {
					m.query = ""
					m.cursor = 0
				} else if m.pickingForEntry {
					m.screen = clockifyEditor
					m.cursor = m.entryCursor
					m.pickingForEntry = false
				} else {
					return m, tea.Quit
				}
			case "s":
				if m.pickingForEntry {
					m.screen = clockifyEditor
					m.cursor = m.entryCursor
					m.pickingForEntry = false
				} else {
					m.done = true
					return m, tea.Quit
				}
			case "enter":
				if len(projects) > 0 {
					m.selected = projects[min(m.cursor, len(projects)-1)].ID
					if m.pickingForEntry {
						m.queue[m.entryCursor].Entry.ProjectID = m.selected
						m.cursor = m.entryCursor
						m.screen = clockifyEditor
						m.pickingForEntry = false
						m.query = ""
					} else {
						m.done = true
						return m, tea.Quit
					}
				}
			}
		case clockifyReview:
			switch key {
			case "esc":
				return m, tea.Quit
			case "up", "k":
				m.cursor = wrapIndex(m.cursor-1, len(m.queue))
			case "down", "j":
				m.cursor = wrapIndex(m.cursor+1, len(m.queue))
			case "enter", "e":
				if len(m.queue) > 0 {
					m.screen = clockifyEditor
				}
			case "s":
				if len(m.queue) > 0 {
					m.queue = append(m.queue[:m.cursor], m.queue[m.cursor+1:]...)
					m.cursor = min(m.cursor, max(0, len(m.queue)-1))
				}
			case "p":
				if m.dryRun || len(m.queue) == 0 {
					m.done = true
					return m, tea.Quit
				}
				m.screen = clockifyConfirm
			}
		case clockifyEditor:
			switch key {
			case "esc", "enter":
				m.screen = clockifyReview
				m.message = ""
			case "p":
				m.entryCursor = m.cursor
				m.local = m.description(m.queue[m.cursor])
				m.pickingForEntry = true
				m.screen = clockifyPicker
				m.cursor = 0
				m.query = ""
			case "d":
				m.field = "description"
				m.input = m.description(m.queue[m.cursor])
			case "t":
				m.field = "duration"
				m.input = (time.Duration(m.queue[m.cursor].ExportSeconds) * time.Second).String()
			case "b":
				if m.provider == "Clockify" {
					m.queue[m.cursor].Entry.Billable = !m.queue[m.cursor].Entry.Billable
				}
			}
		case clockifyConfirm:
			switch key {
			case "esc", "n":
				m.screen = clockifyReview
			case "y":
				m.done = true
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m *clockifyModel) applyEdit() {
	r := &m.queue[m.cursor]
	switch m.field {
	case "description":
		marker := " [burrowtime:" + r.FrameID + "]"
		value := strings.TrimSpace(strings.ReplaceAll(m.input, marker, ""))
		if strings.TrimSpace(m.input) == "" || len([]rune(value)) > 3000 {
			m.message = "Enter a description of at most 3000 characters."
			return
		}
		r.Entry.Description = value
	case "duration":
		d, err := time.ParseDuration(m.input)
		if err != nil || d <= 0 || d%time.Second != 0 {
			m.message = "Use a positive duration in whole seconds, e.g. 1h15m."
			return
		}
		start, err := time.Parse(time.RFC3339, r.Entry.Start)
		if err != nil {
			m.message = "Invalid entry start time."
			return
		}
		r.Entry.End = start.Add(d).UTC().Format(time.RFC3339)
		r.ExportSeconds = int64(d / time.Second)
	}
	m.field = ""
	m.message = ""
}

func (m clockifyModel) description(r integrations.Receipt) string {
	return strings.TrimSpace(strings.ReplaceAll(r.Entry.Description, " [burrowtime:"+r.FrameID+"]", ""))
}
func (m clockifyModel) projectName(id string) string {
	for _, p := range m.projects {
		if p.ID == id {
			return p.ClientLabel() + " / " + p.Name
		}
	}
	return id
}
func (m clockifyModel) listRows() int { return max(1, (m.height-21)/2) }
func clockifySafe(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}
func clockifyClip(s string, width int) string {
	return ansi.Truncate(clockifySafe(s), max(1, width), "…")
}

func (m clockifyModel) View() string {
	if m.width < 40 || m.height < 24 {
		return tuiMutedStyle.Render("Enlarge terminal to 40 × 24.\nCtrl+C cancels sync.")
	}
	w := min(110, m.width-4)
	inner := w - 4
	brand := tuiBrandStyle.Render("◷  BURROWTIME") + tuiMutedStyle.Render("  /  ") + tuiTitleStyle.Render(m.provider)
	label := "PROJECT MAPPING"
	if m.screen != clockifyPicker {
		label = "REVIEW EXPORT"
	}
	if m.dryRun {
		label = "DRY RUN"
	}
	header := brand + "\n" + tuiMutedStyle.Render(label)
	var body, help string
	switch m.screen {
	case clockifyPicker:
		projects := m.filtered()
		context := tuiTagStyle.Render(clockifyClip(m.local, inner)) + "\n" + tuiMutedStyle.Render("Choose its "+m.provider+" destination")
		search := "/ Search projects"
		if m.query != "" || m.searching {
			search = "/ " + m.query
		}
		if m.searching {
			search += " ▏"
		}
		lines := []string{tuiBrandStyle.Render(clockifyClip(search, inner)), ""}
		rows := m.listRows()
		cursor := min(m.cursor, max(0, len(projects)-1))
		start := max(0, cursor-rows+1)
		for i := start; i < min(len(projects), start+rows); i++ {
			p := projects[i]
			name := clockifyClip(p.Name, inner-5)
			prefix := "  "
			style := tuiTitleStyle
			if i == cursor {
				prefix = "› "
				style = style.Foreground(tuiPrimary).Background(tuiSurface).Bold(true)
			}
			lines = append(lines, style.Width(inner).Render(prefix+name))
			detail := "  Client: " + p.ClientLabel()
			if projectScore(m.local, p.Name) >= 4000 {
				detail += " · suggested"
			}
			lines = append(lines, tuiMutedStyle.Render(clockifyClip(detail, inner)))
		}
		if len(projects) == 0 {
			lines = append(lines, tuiMutedStyle.Render("No matching projects. Esc clears search."))
		}
		lines = append(lines, "", tuiMutedStyle.Render(fmt.Sprintf("%d of %d projects", min(cursor+1, len(projects)), len(projects))))
		body = tuiPanel("LOCAL PROJECT", context, w) + "\n" + tuiPanel("CLOCKIFY PROJECTS", strings.Join(lines, "\n"), w)
		help = tuiHelp("↑/↓", "move", "enter", "map", "/", "search", "s", "skip", "q", "cancel")
		if m.searching {
			help = tuiHelp("type", "filter", "enter", "browse", "esc", "back")
		}
	case clockifyReview:
		var recorded, exported int64
		for _, r := range m.queue {
			recorded += r.RecordedSeconds
			exported += r.ExportSeconds
		}
		metrics := fmt.Sprintf("%d entries   %s recorded → %s export", len(m.queue), formatDuration(recorded), formatDuration(exported))
		lines := []string{}
		rows := max(1, (m.height-16)/2)
		start := max(0, m.cursor-rows+1)
		for i := start; i < min(len(m.queue), start+rows); i++ {
			r := m.queue[i]
			prefix := "  "
			style := tuiTitleStyle
			if i == m.cursor {
				prefix = "› "
				style = style.Foreground(tuiPrimary).Background(tuiSurface)
			}
			lines = append(lines, style.Width(inner).Render(clockifyClip(prefix+m.description(r), inner)))
			detail := m.projectName(r.Entry.ProjectID) + " · " + formatDuration(r.RecordedSeconds) + " → " + formatDuration(r.ExportSeconds)
			lines = append(lines, tuiMutedStyle.Render(clockifyClip("  "+detail, inner)))
		}
		if len(m.queue) == 0 {
			lines = append(lines, tuiMutedStyle.Render("All entries skipped. Nothing will be uploaded."))
		}
		body = tuiPanel("BATCH TOTAL", tuiTimeStyle.Render(clockifyClip(metrics, inner)), w) + "\n" + tuiPanel("ENTRIES", strings.Join(lines, "\n"), w)
		if len(m.queue) > 0 {
			r := m.queue[m.cursor]
			body += "\n" + tuiMutedStyle.Render(clockifyClip(r.Entry.Start+" → "+r.Entry.End, w))
		}
		action := "push"
		if m.dryRun {
			action = "finish preview"
		}
		help = tuiHelp("↑/↓", "move", "enter", "edit", "s", "skip", "p", action, "q", "cancel")
	case clockifyEditor:
		r := m.queue[m.cursor]
		lines := []string{
			tuiTagStyle.Render(clockifyClip(m.description(r), inner)), "",
			tuiBrandStyle.Render("p  ") + "Project     " + clockifyClip(m.projectName(r.Entry.ProjectID), inner-15),
			tuiBrandStyle.Render("d  ") + "Description " + clockifyClip(m.description(r), inner-15),
			tuiBrandStyle.Render("t  ") + "Duration    " + tuiTimeStyle.Render(formatDuration(r.ExportSeconds)),
		}
		if m.provider == "Clockify" {
			lines = append(lines, tuiBrandStyle.Render("b  ")+fmt.Sprintf("Billable    %t", r.Entry.Billable))
		}
		lines = append(lines, "", tuiMutedStyle.Render(clockifyClip("Recorded: "+formatDuration(r.RecordedSeconds)+" · local data stays exact", inner)))
		body = tuiPanel("EDIT EXPORT", strings.Join(lines, "\n"), w)
		if m.field != "" {
			body += "\n" + tuiPanel(strings.ToUpper(m.field), tuiBrandStyle.Render(ansi.TruncateLeft(clockifySafe(m.input), max(1, inner-2), "…")+" ▏"), w)
		}
		if m.message != "" {
			body += "\n" + tuiMutedStyle.Render(clockifyClip(m.message, w))
		}
		keys := "p/d/t"
		if m.provider == "Clockify" {
			keys += "/b"
		}
		help = tuiHelp(keys, "edit field", "enter/esc", "back", "q", "cancel")
		if m.field != "" {
			help = tuiHelp("enter", "save", "ctrl+u", "clear", "esc", "discard")
		}
	case clockifyConfirm:
		var seconds int64
		for _, r := range m.queue {
			seconds += r.ExportSeconds
		}
		body = tuiPanel("CONFIRM UPLOAD", tuiTitleStyle.Render(fmt.Sprintf("Push %d entries to %s?", len(m.queue), m.provider))+"\n\n"+tuiTimeStyle.Render(formatDuration(seconds))+"\n\n"+tuiMutedStyle.Render("This creates real "+m.provider+" time entries."), w)
		help = tuiHelp("y", "confirm push", "esc/n", "back", "q", "cancel")
	}
	// Wrap help without widening a small terminal. Each panel remains bounded.
	help = lipgloss.NewStyle().Width(w).Render(help)
	return lipgloss.NewStyle().Padding(1, 2).Render(header + "\n\n" + body + "\n\n" + help)
}
