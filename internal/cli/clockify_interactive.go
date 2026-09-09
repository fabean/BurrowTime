package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/fabean/BurrowTime/internal/integrations"
	"github.com/fabean/BurrowTime/internal/store"
)

type exportPrompter struct {
	in       io.Reader
	out      io.Writer
	projects []integrations.Project
	dryRun   bool
	run      func(clockifyModel) (clockifyModel, error)
}

func projectWords(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }), " ")
}

// Prefer normalized exact names, containment, then word overlap and edit distance.
func projectScore(local, remote string) int {
	a, b := projectWords(local), projectWords(remote)
	if a == b {
		return 10000
	}
	if a == "" || b == "" {
		return 0
	}
	score := 0
	if strings.Contains(a, b) || strings.Contains(b, a) {
		score += 4000
	}
	words := map[string]bool{}
	for _, w := range strings.Fields(a) {
		words[w] = true
	}
	for _, w := range strings.Fields(b) {
		if words[w] {
			score += 500
		}
	}
	ar, br := []rune(a), []rune(b)
	// Bound comparisons for unusually long remote names.
	if len(ar) > 200 {
		ar = ar[:200]
	}
	if len(br) > 200 {
		br = br[:200]
	}
	row := make([]int, len(br)+1)
	for j := range row {
		row[j] = j
	}
	for i, x := range ar {
		prev := row[0]
		row[0] = i + 1
		for j, y := range br {
			old := row[j+1]
			cost := 1
			if x == y {
				cost = 0
			}
			row[j+1] = min(row[j]+1, row[j+1]+1, prev+cost)
			prev = old
		}
	}
	return score + max(0, 200-row[len(br)])
}

func (p *exportPrompter) mapMissing(config *integrations.Config, frames []store.Frame, opts *integrations.Options) error {
	names := map[string]bool{}
	for _, f := range frames {
		if f.Stop == nil {
			continue
		}
		start := time.Unix(f.Start, 0)
		if (!opts.From.IsZero() && start.Before(opts.From)) || (!opts.To.IsZero() && !start.Before(opts.To)) {
			continue
		}
		if _, ok := config.Projects[f.Project]; !ok {
			names[f.Project] = true
		}
	}
	ordered := []string{}
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	opts.SkipProjects = map[string]bool{}
	for _, name := range ordered {
		id, err := p.chooseProject(name)
		if err != nil {
			return err
		}
		if id == "" {
			opts.SkipProjects[name] = true
			fmt.Fprintf(p.out, "Skipping %q for this run.\n", name)
			continue
		}
		config.Projects[name] = integrations.Mapping{Connection: opts.Connection, ProjectID: id}
	}
	return nil
}

func (p *exportPrompter) start(m clockifyModel) (clockifyModel, error) {
	if p.run != nil {
		return p.run(m)
	}
	model, err := tea.NewProgram(m, tea.WithInput(p.in), tea.WithOutput(p.out), tea.WithAltScreen()).Run()
	if err != nil {
		return m, err
	}
	result := model.(clockifyModel)
	if !result.done {
		return result, fmt.Errorf("sync canceled without uploading")
	}
	return result, nil
}

func (p *exportPrompter) chooseProject(local string) (string, error) {
	m := newClockifyModel(p.projects)
	m.local = local
	m.dryRun = p.dryRun
	m.screen = clockifyPicker
	result, err := p.start(m)
	if err != nil {
		return "", err
	}
	return result.selected, nil
}

func (p *exportPrompter) review(queue []integrations.Receipt) ([]integrations.Receipt, error) {
	m := newClockifyModel(p.projects)
	m.screen = clockifyReview
	m.queue = append([]integrations.Receipt(nil), queue...)
	m.dryRun = p.dryRun
	result, err := p.start(m)
	if err != nil {
		return nil, err
	}
	return result.queue, nil
}
