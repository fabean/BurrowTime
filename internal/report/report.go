package report

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/fabean/BurrowTime/internal/store"
)

type Options struct {
	From, To                                   time.Time
	Projects, Tags, IgnoreProjects, IgnoreTags []string
	IncludeCurrent                             bool
	IncludePartial                             bool
}

type Seconds float64

func (s Seconds) MarshalJSON() ([]byte, error) { return []byte(FormatSeconds(s)), nil }
func FormatSeconds(s Seconds) string {
	text := strconv.FormatFloat(float64(s), 'f', -1, 64)
	if !strings.Contains(text, ".") {
		text += ".0"
	}
	return text
}

type TagTotal struct {
	Name string  `json:"name"`
	Time Seconds `json:"time"`
}
type ProjectTotal struct {
	Name string     `json:"name"`
	Tags []TagTotal `json:"tags"`
	Time Seconds    `json:"time"`
}
type Timespan struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

func (t Timespan) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`{"from":%q,"to":%q}`, iso(t.From), iso(t.To))), nil
}

type Summary struct {
	Projects []ProjectTotal `json:"projects"`
	Time     Seconds        `json:"time"`
	Timespan Timespan       `json:"timespan"`
}

func DayBounds(from, to time.Time) (time.Time, time.Time) {
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, from.Location())
	to = time.Date(to.Year(), to.Month(), to.Day()+1, 0, 0, 0, 0, to.Location()).Add(-time.Microsecond)
	return from, to
}

func Filter(frames []store.Frame, current store.State, now time.Time, o Options) []store.Frame {
	timers := []store.ActiveTimer{}
	if current.Running() {
		timers = append(timers, store.ActiveTimer{ID: "primary", Project: current.Project, Start: current.Start, Tags: current.Tags, Primary: true})
	}
	return FilterActive(frames, timers, now, o)
}

// FilterActive is Filter's concurrent-timer counterpart. Completed frames are
// still ordinary Watson frames; running timers are projected only when the
// query requests current activity.
func FilterActive(frames []store.Frame, current []store.ActiveTimer, now time.Time, o Options) []store.Frame {
	all := append([]store.Frame(nil), frames...)
	if o.IncludeCurrent {
		for _, timer := range current {
			if !timer.Running() {
				continue
			}
			stop := now.Unix()
			id := "current"
			if !timer.Primary && timer.ID != "" {
				id = timer.ID
			}
			all = append(all, store.Frame{Start: timer.Start, Stop: &stop, StopMicros: now.Nanosecond() / 1000, Project: timer.Project, ID: id, Tags: append([]string{}, timer.Tags...), UpdatedAt: now.Unix()})
		}
	}
	projects, tags, ignoreProjects, ignoreTags := sets(o.Projects), sets(o.Tags), sets(o.IgnoreProjects), sets(o.IgnoreTags)
	start, stop := o.From.Unix(), o.To.Unix()
	out := []store.Frame{}
	for _, f := range all {
		if f.Stop == nil {
			continue
		}
		if len(projects) > 0 && !projects[f.Project] {
			continue
		}
		if ignoreProjects[f.Project] {
			continue
		}
		if len(tags) > 0 && !hasAny(f.Tags, tags) {
			continue
		}
		if hasAny(f.Tags, ignoreTags) {
			continue
		}
		if f.Start >= start && *f.Stop <= stop {
			out = append(out, f)
			continue
		}
		if o.IncludePartial && f.Start <= stop && *f.Stop >= start {
			copy := f
			if copy.Start < start {
				copy.Start = start
				copy.StartMicros = o.From.Nanosecond() / 1000
			}
			if *copy.Stop > stop {
				v := stop
				copy.Stop = &v
				copy.StopMicros = o.To.Nanosecond() / 1000
			}
			out = append(out, copy)
		}
	}
	return out
}

func Build(frames []store.Frame, o Options) Summary {
	byProject := map[string][]store.Frame{}
	for _, f := range frames {
		byProject[f.Project] = append(byProject[f.Project], f)
	}
	names := make([]string, 0, len(byProject))
	for name := range byProject {
		names = append(names, name)
	}
	sort.Strings(names)
	summary := Summary{Projects: []ProjectTotal{}, Timespan: Timespan{From: o.From, To: o.To}}
	requestedTags := sets(o.Tags)
	for _, name := range names {
		fs := byProject[name]
		p := ProjectTotal{Name: name, Tags: []TagTotal{}}
		tagTimes := map[string]float64{}
		for _, f := range fs {
			seconds := float64(*f.Stop-f.Start) + float64(f.StopMicros-f.StartMicros)/1_000_000
			p.Time += Seconds(seconds)
			for _, tag := range f.Tags {
				if len(requestedTags) == 0 || requestedTags[tag] {
					tagTimes[tag] += seconds
				}
			}
		}
		tagNames := make([]string, 0, len(tagTimes))
		for tag := range tagTimes {
			tagNames = append(tagNames, tag)
		}
		sort.Strings(tagNames)
		for _, tag := range tagNames {
			p.Tags = append(p.Tags, TagTotal{Name: tag, Time: Seconds(tagTimes[tag])})
		}
		summary.Time += p.Time
		summary.Projects = append(summary.Projects, p)
	}
	return summary
}

func iso(t time.Time) string { return t.Format("2006-01-02T15:04:05.999999-07:00") }

func sets(items []string) map[string]bool {
	m := map[string]bool{}
	for _, v := range items {
		m[v] = true
	}
	return m
}
func hasAny(items []string, set map[string]bool) bool {
	for _, v := range items {
		if set[v] {
			return true
		}
	}
	return false
}
