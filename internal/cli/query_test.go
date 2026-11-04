package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/fabean/BurrowTime/internal/config"
	"github.com/fabean/BurrowTime/internal/store"
	"github.com/fabean/BurrowTime/internal/watson"
	"github.com/spf13/cobra"
)

func TestQueryOptionsTimespansAndValidation(t *testing.T) {
	t.Setenv("TZ", "UTC")
	now := time.Date(2026, 11, 4, 14, 30, 0, 0, time.UTC)
	s := &watson.Service{Now: func() time.Time { return now }, Config: config.New()}
	if err := s.Config.Set("options", "week_start", "sunday"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		q         queryFlags
		from      string
		wantError string
	}{
		{name: "default", from: "2026-10-28"},
		{name: "day", q: queryFlags{day: true}, from: "2026-11-04"},
		{name: "week starts sunday", q: queryFlags{week: true}, from: "2026-11-01"},
		{name: "month", q: queryFlags{month: true}, from: "2026-11-01"},
		{name: "year", q: queryFlags{year: true}, from: "2026-01-01"},
		{name: "all", q: queryFlags{all: true}, from: "1970-01-01"},
		{name: "explicit", q: queryFlags{from: "2026-10-01", to: "2026-10-02"}, from: "2026-10-01"},
		{name: "bad from", q: queryFlags{from: "no date"}, wantError: "Invalid time"},
		{name: "bad to", q: queryFlags{to: "no date"}, wantError: "Invalid time"},
		{name: "multiple shortcuts", q: queryFlags{day: true, all: true}, wantError: "mutually exclusive"},
		{name: "shortcut and date", q: queryFlags{day: true, from: "2026-11-01"}, wantError: "mutually exclusive"},
		{name: "reversed", q: queryFlags{from: "2026-11-05"}, wantError: "anterior"},
		{name: "project excluded", q: queryFlags{projects: []string{"a"}, ignoreProjects: []string{"a"}}, wantError: "projects can't"},
		{name: "tag excluded", q: queryFlags{tags: []string{"a"}, ignoreTags: []string{"a"}}, wantError: "tags can't"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.q.options(s, true)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("error=%v, want %q", err, tc.wantError)
				}
				return
			}
			if err != nil || got.From.Format("2006-01-02") != tc.from || !got.IncludePartial {
				t.Fatalf("options=%+v err=%v", got, err)
			}
		})
	}
	if err := s.Config.Set("options", "report_current", "true"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		q    queryFlags
		want bool
	}{
		{name: "configured", want: true},
		{name: "explicit off", q: queryFlags{noCurrent: true}},
		{name: "explicit on", q: queryFlags{current: true}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.q.options(s, true)
			if err != nil || got.IncludeCurrent != tc.want {
				t.Fatalf("current=%v err=%v", got.IncludeCurrent, err)
			}
		})
	}
}

func TestQueryCommandsFormatsFiltersAndMessages(t *testing.T) {
	t.Setenv("TZ", "UTC")
	dir := t.TempDir()
	for _, args := range [][]string{
		{"add", "alpha", "+ticket", "--from", "2020-01-02 09:00", "--to", "2020-01-02 10:00", "-m", "fixed issue"},
		{"add", "beta", "+review", "--from", "2020-01-03 10:00", "--to", "2020-01-03 10:30"},
	} {
		if out, err := runBurrowTimeCommand(dir, args...); err != nil {
			t.Fatalf("seed: %v: %s", err, out)
		}
	}
	for _, command := range []string{"log", "report", "aggregate"} {
		for _, format := range []string{"--json", "--csv", "--plain"} {
			t.Run(command+format, func(t *testing.T) {
				out, err := runBurrowTimeCommand(
					dir, command, "--from", "2020-01-02", "--to", "2020-01-03", "--no-pager", format,
				)
				if err != nil || !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
					t.Fatalf("out=%q err=%v", out, err)
				}
				if format == "--json" && !json.Valid([]byte(out)) {
					t.Fatalf("invalid JSON: %s", out)
				}
				if format == "--csv" {
					rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
					if err != nil || len(rows) < 3 {
						t.Fatalf("CSV rows=%v err=%v", rows, err)
					}
				}
			})
		}
	}
	out, err := runBurrowTimeCommand(
		dir, "aggregate", "--from", "2020-01-02", "--to", "2020-01-03", "--show-messages",
	)
	if err != nil || !strings.Contains(out, "↳ fixed issue") {
		t.Fatalf("messages=%q err=%v", out, err)
	}
	for _, order := range []string{"--reverse", "--no-reverse"} {
		out, err := runBurrowTimeCommand(dir, "log", "--all", order)
		if err != nil || (strings.Index(out, "alpha") < strings.Index(out, "beta")) != (order == "--no-reverse") {
			t.Fatalf("order %s: %q err=%v", order, out, err)
		}
	}
	out, err = runBurrowTimeCommand(dir, "report", "--all", "--project", "alpha", "--ignore-tag", "review")
	if err != nil || !strings.Contains(out, "1h 00m 00s") || strings.Contains(out, "beta") {
		t.Fatalf("filtered=%q err=%v", out, err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestQueryExportErrorsAndUnicode(t *testing.T) {
	stop := int64(20)
	frames := []store.Frame{{ID: "abcdefgh", Start: 10, Stop: &stop, Project: "café 😀", Tags: []string{"täg"}, Message: "a,\"b\"\nc"}}
	var output bytes.Buffer
	if err := writeLogJSON(&output, frames, time.UTC); err != nil || !json.Valid(output.Bytes()) || !strings.Contains(output.String(), `\ud83d\ude00`) {
		t.Fatalf("JSON=%q err=%v", output.String(), err)
	}
	output.Reset()
	if err := writeLogCSV(&output, frames, time.UTC); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&output).ReadAll()
	if err != nil || rows[1][5] != frames[0].Message {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{name: "JSON writer", run: func() error { return writeLogJSON(failingWriter{}, frames, time.UTC) }},
		{name: "CSV writer", run: func() error { return writeLogCSV(failingWriter{}, frames, time.UTC) }},
		{name: "empty CSV writer", run: func() error { return writeLogCSV(failingWriter{}, nil, time.UTC) }},
		{name: "JSON type", run: func() error { return encodeJSON(&output, make(chan int)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil {
				t.Fatal("expected export failure")
			}
		})
	}
	cmd := &cobra.Command{}
	cmd.SetOut(failingWriter{})
	if err := emitPaged(cmd, []byte("report"), true); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write error=%v", err)
	}
}

func TestQueryPairedFlagsUseLastSelection(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		check func(queryFlags) bool
	}{
		{name: "current", args: []string{"--no-current", "--current"}, check: func(q queryFlags) bool { return q.current && !q.noCurrent }},
		{name: "no current", args: []string{"--current", "--no-current"}, check: func(q queryFlags) bool { return !q.current && q.noCurrent }},
		{name: "plain", args: []string{"--json", "--plain"}, check: func(q queryFlags) bool { return !q.json && !q.csv }},
		{name: "pager", args: []string{"--no-pager", "--pager"}, check: func(q queryFlags) bool { return q.pager && !q.noPager }},
		{name: "no pager", args: []string{"--pager", "--no-pager"}, check: func(q queryFlags) bool { return !q.pager && q.noPager }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var q queryFlags
			cmd := &cobra.Command{}
			q.bind(cmd, true, true)
			if err := cmd.ParseFlags(tc.args); err != nil || !tc.check(q) {
				t.Fatalf("flags=%+v err=%v", q, err)
			}
		})
	}
	s := &watson.Service{Config: config.New()}
	if !(queryFlags{pager: true}).usePager(s) || (queryFlags{noPager: true}).usePager(s) {
		t.Fatal("explicit pager selection ignored")
	}
}

func TestAggregateShowsUntaggedMessagesWithTaggedSessions(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"add", "project", "+ticket", "--from", "2020-01-02 09:00", "--to", "2020-01-02 10:00", "-m", "tagged work"},
		{"add", "project", "--from", "2020-01-02 10:00", "--to", "2020-01-02 11:00", "-m", "untagged work"},
		{"add", "project", "--from", "2020-01-02 11:00", "--to", "2020-01-02 12:00", "-m", "untagged work"},
	} {
		if _, err := runBurrowTimeCommand(dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	args := []string{"aggregate", "--from", "2020-01-02", "--to", "2020-01-02"}
	hidden, err := runBurrowTimeCommand(dir, args...)
	if err != nil || strings.Contains(hidden, "work") {
		t.Fatalf("hidden=%q err=%v", hidden, err)
	}
	shown, err := runBurrowTimeCommand(dir, append(args, "--show-messages")...)
	if err != nil || strings.Count(shown, "↳ untagged work") != 1 || strings.Count(shown, "↳ tagged work") != 1 || !strings.Contains(shown, "3h 00m 00s") {
		t.Fatalf("shown=%q err=%v", shown, err)
	}
}
