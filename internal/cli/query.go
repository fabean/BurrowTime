package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	btDatetime "github.com/josh/burrowtime/internal/datetime"
	btReport "github.com/josh/burrowtime/internal/report"
	"github.com/josh/burrowtime/internal/store"
	"github.com/josh/burrowtime/internal/watson"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type queryFlags struct {
	from, to                                   string
	current, noCurrent                         bool
	day, week, month, year, luna, all          bool
	projects, tags, ignoreProjects, ignoreTags []string
	json, csv, pager, noPager                  bool
}

func (q *queryFlags) bind(cmd *cobra.Command, shortcuts, ignores bool) {
	f := cmd.Flags()
	f.StringVarP(&q.from, "from", "f", "", "start of the timespan")
	f.StringVarP(&q.to, "to", "t", "", "inclusive end of the timespan")
	f.BoolFuncP("current", "c", "include the currently running frame", func(value string) error {
		enabled, err := strconv.ParseBool(value)
		if err == nil && enabled {
			q.current, q.noCurrent = true, false
		}
		return err
	})
	f.BoolFuncP("no-current", "C", "exclude the currently running frame", func(value string) error {
		enabled, err := strconv.ParseBool(value)
		if err == nil && enabled {
			q.current, q.noCurrent = false, true
		}
		return err
	})
	if shortcuts {
		f.BoolVarP(&q.day, "day", "d", false, "current day")
		f.BoolVarP(&q.week, "week", "w", false, "current week")
		f.BoolVarP(&q.month, "month", "m", false, "current month")
		f.BoolVarP(&q.year, "year", "y", false, "current year")
		f.BoolVarP(&q.luna, "luna", "l", false, "current moon cycle")
		f.BoolVarP(&q.all, "all", "a", false, "all activities")
	}
	f.StringArrayVarP(&q.projects, "project", "p", nil, "include project (repeatable)")
	f.StringArrayVarP(&q.tags, "tag", "T", nil, "include tag (repeatable)")
	if ignores {
		f.StringArrayVar(&q.ignoreProjects, "ignore-project", nil, "exclude project (repeatable)")
		f.StringArrayVar(&q.ignoreTags, "ignore-tag", nil, "exclude tag (repeatable)")
	}
	f.BoolFuncP("json", "j", "output JSON", func(value string) error {
		enabled, err := strconv.ParseBool(value)
		if err == nil && enabled {
			q.json, q.csv = true, false
		}
		return err
	})
	f.BoolFuncP("csv", "s", "output CSV", func(value string) error {
		enabled, err := strconv.ParseBool(value)
		if err == nil && enabled {
			q.json, q.csv = false, true
		}
		return err
	})
	f.BoolFunc("plain", "output plain text", func(value string) error {
		enabled, err := strconv.ParseBool(value)
		if err == nil && enabled {
			q.json, q.csv = false, false
		}
		return err
	})
	_ = f.MarkHidden("plain")
	f.BoolFuncP("pager", "g", "view output through a pager", func(value string) error {
		enabled, err := strconv.ParseBool(value)
		if err == nil && enabled {
			q.pager, q.noPager = true, false
		}
		return err
	})
	f.BoolFuncP("no-pager", "G", "do not use a pager", func(value string) error {
		enabled, err := strconv.ParseBool(value)
		if err == nil && enabled {
			q.pager, q.noPager = false, true
		}
		return err
	})
}

func (q queryFlags) options(s *watson.Service, reportMode bool) (btReport.Options, error) {
	now := s.Now()
	from := now.AddDate(0, 0, -7)
	to := now
	if q.from != "" {
		v, e := parseTime(q.from, now)
		if e != nil {
			return btReport.Options{}, e
		}
		from = v
	}
	if q.to != "" {
		v, e := parseTime(q.to, now)
		if e != nil {
			return btReport.Options{}, e
		}
		to = v
	}
	shortcuts := 0
	for _, v := range []bool{q.day, q.week, q.month, q.year, q.luna, q.all} {
		if v {
			shortcuts++
		}
	}
	if shortcuts > 1 {
		return btReport.Options{}, errors.New("timespan shortcut options are mutually exclusive")
	}
	if shortcuts > 0 && (q.from != "" || q.to != "") {
		return btReport.Options{}, errors.New("--from/--to and timespan shortcuts are mutually exclusive")
	}
	local := now.Location()
	if q.day {
		from = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, local)
	}
	if q.week {
		today := (int(now.Weekday()) + 6) % 7
		desired := 0
		weekdays := map[string]int{"monday": 0, "tuesday": 1, "wednesday": 2, "thursday": 3, "friday": 4, "saturday": 5, "sunday": 6}
		if configured, ok := weekdays[strings.ToLower(s.Config.Get("options", "week_start", "monday"))]; ok {
			desired = configured
		}
		offset := desired
		if desired > today {
			offset -= 7
		}
		from = time.Date(now.Year(), now.Month(), now.Day()-today+offset, 0, 0, 0, 0, local)
	}
	if q.month {
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, local)
	}
	if q.year {
		from = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, local)
	}
	if q.luna {
		moon, err := btDatetime.LastFullMoon(now)
		if err != nil {
			return btReport.Options{}, err
		}
		from = moon
	}
	if q.all {
		from = time.Date(1970, 1, 1, 0, 0, 0, 0, local)
	}
	from, to = btReport.DayBounds(from, to)
	if from.After(to) {
		return btReport.Options{}, errors.New("'from' must be anterior to 'to'")
	}
	if overlap(q.projects, q.ignoreProjects) {
		return btReport.Options{}, errors.New("given projects can't be ignored at the same time")
	}
	if overlap(q.tags, q.ignoreTags) {
		return btReport.Options{}, errors.New("given tags can't be ignored at the same time")
	}
	includeCurrent := q.current
	if !q.current && !q.noCurrent {
		key := "log_current"
		if reportMode {
			key = "report_current"
		}
		includeCurrent = s.Config.Bool("options", key, false)
	}
	return btReport.Options{From: from, To: to, Projects: q.projects, Tags: q.tags, IgnoreProjects: q.ignoreProjects, IgnoreTags: q.ignoreTags, IncludeCurrent: includeCurrent, IncludePartial: reportMode}, nil
}

func overlap(a, b []string) bool {
	set := map[string]bool{}
	for _, v := range a {
		set[v] = true
	}
	for _, v := range b {
		if set[v] {
			return true
		}
	}
	return false
}

func (a *app) log() *cobra.Command {
	q := &queryFlags{}
	var reverse, noReverse bool
	cmd := &cobra.Command{Use: "log", Short: "Display each recorded session during the given timespan.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, e := a.open()
		if e != nil {
			return e
		}
		o, e := q.options(s, false)
		if e != nil {
			return e
		}
		frames := btReport.FilterActive(s.Frames, s.RunningTimers(), s.Now(), o)
		displayLoc := watsonDisplayLocation(s.Now())
		if q.json {
			return writeLogJSON(cmd.OutOrStdout(), frames, displayLoc)
		}
		if q.csv {
			return writeLogCSV(cmd.OutOrStdout(), frames, displayLoc)
		}
		rev := reverse
		if !reverse && !noReverse {
			rev = s.Config.Bool("options", "reverse_log", true)
		}
		var output bytes.Buffer
		writeLogPlain(&output, frames, displayLoc, rev)
		return emitPaged(cmd, output.Bytes(), q.usePager(s))
	}}
	q.bind(cmd, true, true)
	a.bindQueryCompletions(cmd)
	cmd.Flags().BoolFuncP("reverse", "r", "reverse day order", func(value string) error {
		enabled, err := strconv.ParseBool(value)
		if err == nil && enabled {
			reverse, noReverse = true, false
		}
		return err
	})
	cmd.Flags().BoolFuncP("no-reverse", "R", "chronological day order", func(value string) error {
		enabled, err := strconv.ParseBool(value)
		if err == nil && enabled {
			reverse, noReverse = false, true
		}
		return err
	})
	return cmd
}

func (a *app) report() *cobra.Command {
	q := &queryFlags{}
	cmd := &cobra.Command{Use: "report", Short: "Display a report of time spent on each project.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, e := a.open()
		if e != nil {
			return e
		}
		o, e := q.options(s, true)
		if e != nil {
			return e
		}
		frames := btReport.FilterActive(s.Frames, s.RunningTimers(), s.Now(), o)
		summary := btReport.Build(frames, o)
		if q.json {
			return encodeJSON(cmd.OutOrStdout(), summary)
		}
		if q.csv {
			return writeReportCSV(cmd.OutOrStdout(), summary)
		}
		var output bytes.Buffer
		writeReportPlain(&output, summary)
		return emitPaged(cmd, output.Bytes(), q.usePager(s))
	}}
	q.bind(cmd, true, true)
	a.bindQueryCompletions(cmd)
	return cmd
}

func (a *app) aggregate() *cobra.Command {
	q := &queryFlags{}
	cmd := &cobra.Command{Use: "aggregate", Short: "Display time by project aggregated by day.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, e := a.open()
		if e != nil {
			return e
		}
		o, e := q.options(s, true)
		if e != nil {
			return e
		}
		summaries := []btReport.Summary{}
		for day := o.From; !day.After(o.To); day = day.AddDate(0, 0, 1) {
			dayFrom, dayTo := btReport.DayBounds(day, day)
			daily := o
			daily.From, daily.To = dayFrom, dayTo
			frames := btReport.FilterActive(s.Frames, s.RunningTimers(), s.Now(), daily)
			summaries = append(summaries, btReport.Build(frames, daily))
		}
		if q.json {
			return encodeJSON(cmd.OutOrStdout(), summaries)
		}
		if q.csv {
			return writeAggregateCSV(cmd.OutOrStdout(), summaries)
		}
		var output bytes.Buffer
		writeAggregatePlain(&output, summaries)
		return emitPaged(cmd, output.Bytes(), q.usePager(s))
	}}
	q.bind(cmd, false, false)
	a.bindQueryCompletions(cmd)
	return cmd
}

type logJSONFrame struct {
	ID      string   `json:"id"`
	Project string   `json:"project"`
	Start   string   `json:"start"`
	Stop    string   `json:"stop"`
	Tags    []string `json:"tags"`
}

func writeLogJSON(w io.Writer, frames []store.Frame, loc *time.Location) error {
	out := make([]logJSONFrame, 0, len(frames))
	for _, f := range frames {
		out = append(out, logJSONFrame{ID: f.ID, Project: f.Project, Start: isoTime(time.Unix(f.Start, int64(f.StartMicros)*1000).In(loc)), Stop: isoTime(time.Unix(*f.Stop, int64(f.StopMicros)*1000).In(loc)), Tags: f.Tags})
	}
	return encodeJSON(w, out)
}
func writeLogCSV(w io.Writer, frames []store.Frame, loc *time.Location) error {
	if len(frames) == 0 {
		_, err := fmt.Fprintln(w)
		return err
	}
	cw := csv.NewWriter(w)
	if e := cw.Write([]string{"id", "start", "stop", "project", "tags"}); e != nil {
		return e
	}
	for _, f := range frames {
		if e := cw.Write([]string{shortID(f.ID), time.Unix(f.Start, 0).In(loc).Format("2006-01-02 15:04:05"), time.Unix(*f.Stop, 0).In(loc).Format("2006-01-02 15:04:05"), f.Project, strings.Join(f.Tags, ", ")}); e != nil {
			return e
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w)
	return err
}

func writeLogPlain(w io.Writer, frames []store.Frame, loc *time.Location, reverse bool) {
	if len(frames) == 0 {
		fmt.Fprintln(w)
		return
	}
	byDay := map[string][]store.Frame{}
	days := []string{}
	for _, f := range frames {
		day := time.Unix(f.Start, 0).In(loc).Format("2006-01-02")
		if _, ok := byDay[day]; !ok {
			days = append(days, day)
		}
		byDay[day] = append(byDay[day], f)
	}
	sort.Strings(days)
	if reverse {
		for i, j := 0, len(days)-1; i < j; i, j = i+1, j-1 {
			days[i], days[j] = days[j], days[i]
		}
	}
	for dayIndex, day := range days {
		if dayIndex > 0 {
			fmt.Fprintln(w)
		}
		fs := byDay[day]
		sort.SliceStable(fs, func(i, j int) bool { return fs[i].Start < fs[j].Start })
		longestProject := 0
		for _, frame := range fs {
			if width := utf8.RuneCountInString(frame.Project); width > longestProject {
				longestProject = width
			}
		}
		total := int64(0)
		for _, f := range fs {
			total += *f.Stop - f.Start
		}
		date, _ := time.ParseInLocation("2006-01-02", day, loc)
		fmt.Fprintf(w, "%s (%s)\n", styleText("date", date.Format("Monday 02 January 2006")), styleText("time", formatDuration(total)))
		for _, f := range fs {
			start, stop := time.Unix(f.Start, 0).In(loc), time.Unix(*f.Stop, 0).In(loc)
			project := fmt.Sprintf("%*s", longestProject, f.Project)
			tags := formatTags(f.Tags)
			if tags != "" {
				tags = " " + tags
			}
			fmt.Fprintf(w, "\t%s  %s to %s  %11s  %s%s\n", styleText("id", shortID(f.ID)), styleText("time", start.Format("15:04")), styleText("time", stop.Format("15:04")), formatDuration(*f.Stop-f.Start), styleText("project", project), tags)
		}
	}
}

func writeReportCSV(w io.Writer, s btReport.Summary) error {
	if len(s.Projects) == 0 {
		_, err := fmt.Fprintln(w)
		return err
	}
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"from", "to", "project", "tag", "time"})
	from, to := s.Timespan.From.Format("2006-01-02 15:04:05"), s.Timespan.To.Format("2006-01-02 15:04:05")
	for _, p := range s.Projects {
		_ = cw.Write([]string{from, to, p.Name, "", btReport.FormatSeconds(p.Time)})
		for _, tag := range p.Tags {
			_ = cw.Write([]string{from, to, p.Name, tag.Name, btReport.FormatSeconds(tag.Time)})
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w)
	return err
}
func writeReportPlain(w io.Writer, s btReport.Summary) {
	fmt.Fprintf(w, "%s -> %s\n\n", styleText("date", s.Timespan.From.Format("Mon 02 January 2006")), styleText("date", s.Timespan.To.Format("Mon 02 January 2006")))
	for _, p := range s.Projects {
		fmt.Fprintf(w, "%s - %s\n", styleText("project", p.Name), styleText("time", formatDuration(int64(p.Time))))
		// Watson accidentally measures the two keys in each tag record rather
		// than the tag text. Preserve its effective fixed width of two.
		longest := 2
		for _, tag := range p.Tags {
			fmt.Fprintf(w, "\t[%s %s]\n", styleText("tag", fmt.Sprintf("%-*s", longest, tag.Name)), styleText("time", fmt.Sprintf("%11s", formatDuration(int64(tag.Time)))))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "Total: %s\n", styleText("time", formatDuration(int64(s.Time))))
}

func writeAggregateCSV(w io.Writer, summaries []btReport.Summary) error {
	hasRows := false
	for _, s := range summaries {
		if len(s.Projects) > 0 {
			hasRows = true
			break
		}
	}
	if !hasRows {
		_, e := fmt.Fprintln(w)
		return e
	}
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"from", "to", "project", "tag", "time"})
	for _, s := range summaries {
		from, to := s.Timespan.From.Format("2006-01-02 15:04:05"), s.Timespan.To.Format("2006-01-02 15:04:05")
		for _, p := range s.Projects {
			_ = cw.Write([]string{from, to, p.Name, "", btReport.FormatSeconds(p.Time)})
			for _, tag := range p.Tags {
				_ = cw.Write([]string{from, to, p.Name, tag.Name, btReport.FormatSeconds(tag.Time)})
			}
		}
	}
	cw.Flush()
	if e := cw.Error(); e != nil {
		return e
	}
	_, e := fmt.Fprintln(w)
	return e
}

func writeAggregatePlain(w io.Writer, summaries []btReport.Summary) {
	for i, s := range summaries {
		if i > 0 {
			fmt.Fprint(w, "\n\n")
		}
		fmt.Fprintf(w, "%s - %s\n", styleText("date", s.Timespan.From.Format("Mon 02 January 2006")), styleText("time", formatDuration(int64(s.Time))))
		for projectIndex, p := range s.Projects {
			fmt.Fprintf(w, "  %s - %s\n", styleText("project", p.Name), styleText("time", formatDuration(int64(p.Time))))
			longest := 2
			for _, tag := range p.Tags {
				fmt.Fprintf(w, "\t[%s %s]\n", styleText("tag", fmt.Sprintf("%-*s", longest, tag.Name)), styleText("time", fmt.Sprintf("%11s", formatDuration(int64(tag.Time)))))
			}
			if projectIndex < len(s.Projects)-1 {
				fmt.Fprintln(w)
			}
		}
	}
	if len(summaries) > 0 {
		fmt.Fprintln(w)
	}
}
func formatDuration(seconds int64) string {
	negative := seconds < 0
	if negative {
		seconds = -seconds
	}
	total := seconds
	parts := []string{}
	if total >= 3600 {
		hours := seconds / 3600
		parts = append(parts, fmt.Sprintf("%dh", hours))
		seconds -= hours * 3600
	}
	if total >= 60 {
		minutes := seconds / 60
		parts = append(parts, fmt.Sprintf("%02dm", minutes))
		seconds -= minutes * 60
	}
	parts = append(parts, fmt.Sprintf("%02ds", seconds))
	text := strings.Join(parts, " ")
	if negative {
		return "-" + text
	}
	return text
}

func isoTime(t time.Time) string { return t.Format("2006-01-02T15:04:05.999999-07:00") }

func encodeJSON(w io.Writer, value any) error {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetIndent("", "    ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	_, err := w.Write(pythonASCIIJSON(output.Bytes()))
	return err
}

func pythonASCIIJSON(encoded []byte) []byte {
	var output bytes.Buffer
	for len(encoded) > 0 {
		r, size := utf8.DecodeRune(encoded)
		if r < utf8.RuneSelf {
			output.WriteByte(encoded[0])
			encoded = encoded[1:]
			continue
		}
		if r <= 0xffff {
			fmt.Fprintf(&output, `\u%04x`, r)
		} else {
			value := r - 0x10000
			fmt.Fprintf(&output, `\u%04x\u%04x`, 0xd800+(value>>10), 0xdc00+(value&0x3ff))
		}
		encoded = encoded[size:]
	}
	return output.Bytes()
}

func watsonDisplayLocation(now time.Time) *time.Location {
	loc := now.Location()
	if tz := os.Getenv("TZ"); tz != "" {
		if loaded, err := time.LoadLocation(tz); err == nil {
			loc = loaded
		}
	}
	name, offset := now.In(loc).Zone()
	return time.FixedZone(name, offset)
}

func (q queryFlags) usePager(s *watson.Service) bool {
	if q.noPager {
		return false
	}
	if q.pager {
		return true
	}
	return s.Config.Bool("options", "pager", true)
}

func emitPaged(cmd *cobra.Command, data []byte, usePager bool) error {
	out := cmd.OutOrStdout()
	file, ok := out.(*os.File)
	if !usePager || !ok || !term.IsTerminal(int(file.Fd())) {
		_, err := out.Write(data)
		return err
	}
	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less"
	}
	parts := splitCommandLine(pager)
	if len(parts) == 0 {
		_, err := out.Write(data)
		return err
	}
	executable, err := exec.LookPath(parts[0])
	if err != nil {
		_, writeErr := out.Write(data)
		return writeErr
	}
	process := exec.Command(executable, parts[1:]...)
	process.Stdin = bytes.NewReader(data)
	process.Stdout = out
	process.Stderr = cmd.ErrOrStderr()
	process.Env = pagerEnvironment(executable, parts[1:])
	return process.Run()
}

// Click enables ANSI color passthrough when it launches a plain `less`
// pager. Do the same so styled report output is interpreted by less instead
// of showing every escape sequence as literal "ESC[..." text.
func pagerEnvironment(executable string, arguments []string) []string {
	environment := os.Environ()
	if filepath.Base(executable) == "less" && os.Getenv("LESS") == "" && len(arguments) == 0 {
		environment = append(environment, "LESS=-R")
	}
	return environment
}
