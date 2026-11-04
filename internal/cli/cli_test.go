package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fabean/BurrowTime/internal/store"
)

func TestExecuteNoninteractiveCommandsAndUsage(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      []string
		want      string
		wantError string
	}{
		{name: "burrowtime help", want: "Available Commands:"},
		{name: "burrowtime unknown", args: []string{"bogus"}, wantError: "No such command"},
		{name: "burrowtime add", args: []string{"add", "--from=2020-01-01", "--to=2020-01-02", "-project", "+tag"}, want: "Adding project -project"},
		{name: "watson version", args: []string{"--version"}, want: "Watson, version " + WatsonVersion},
		{name: "watson misplaced global", args: []string{"status", "--color"}, wantError: "No such option: --color"},
		{name: "watson shorthand", args: []string{"-v"}, wantError: "No such option: -v"},
		{name: "watson global", args: []string{"--no-color", "status"}, want: "No project started"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			in, err := os.CreateTemp(t.TempDir(), "stdin")
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			out, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			oldArgs, oldIn, oldOut, oldErr := os.Args, os.Stdin, os.Stdout, os.Stderr
			defer func() { os.Args, os.Stdin, os.Stdout, os.Stderr = oldArgs, oldIn, oldOut, oldErr }()
			name := strings.Fields(tc.name)[0]
			os.Args = []string{name}
			os.Stdin, os.Stdout, os.Stderr = in, out, out
			t.Setenv("BURROWTIME_DIR", dir)
			t.Setenv("WATSON_DIR", dir)
			err = Execute(tc.args)
			if tc.wantError != "" {
				if err == nil || ExitCode(err) != 2 || !strings.Contains(FormatError(err), tc.wantError) {
					t.Fatalf("err=%v want %s", err, tc.wantError)
				}
				return
			}
			data, readErr := os.ReadFile(out.Name())
			if err != nil || readErr != nil || !strings.Contains(string(data), tc.want) {
				t.Fatalf("out=%q err=%v readErr=%v", data, err, readErr)
			}
		})
	}
}

func TestStartWithoutProjectDoesNotPromptInScripts(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "bare", args: []string{"start"}},
		{name: "JSON", args: []string{"start", "--json"}},
		{name: "time", args: []string{"start", "--at", "09:00"}},
		{name: "tags only", args: []string{"start", "+ticket"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			out, err := runBurrowTimeCommand(dir, tc.args...)
			if err == nil || err.Error() != "No project given." || out != "" {
				t.Fatalf("output=%q err=%v", out, err)
			}
			if _, err := os.Stat(filepath.Join(dir, "state")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("missing project wrote timer state: %v", err)
			}
		})
	}
}

func TestStartConfirmationAndStopMinuteModes(t *testing.T) {
	dir := t.TempDir()
	if out, err := runBurrowTimeCommandInput(
		dir, "y\ny\n", "start", "project", "+tag", "--confirm-new-project", "--confirm-new-tag",
	); err != nil || !strings.Contains(out, "Starting project") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if out, err := runBurrowTimeCommand(dir, "status", "--elapsed"); err != nil || out == "" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if _, err := runBurrowTimeCommand(dir, "cancel"); err != nil {
		t.Fatal(err)
	}
	if _, err := runBurrowTimeCommand(dir, "config", "options.time_precision", "minute"); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"start", "one"},
		{"start", "two", "--concurrent"},
		{"stop", "--all", "-m", "done"},
		{"start", "three"},
		{"stop", "--timer", "primary", "-m", "done"},
	} {
		if out, err := runBurrowTimeCommand(dir, args...); err != nil {
			t.Fatalf("%v out=%q err=%v", args, out, err)
		}
	}
}

func TestFailedStopPreservesMessages(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags []string
	}{
		{name: "primary"},
		{name: "explicit timer", flags: []string{"--timer", "primary"}},
		{name: "all", flags: []string{"--all"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := runBurrowTimeCommand(dir, "start", "project", "--at", "2020-01-02", "-m", "original"); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"stop", "--at", "2020-01-01", "-m", "replacement"}, tc.flags...)
			if _, err := runBurrowTimeCommand(dir, args...); err == nil {
				t.Fatal("accepted stop before start")
			}
			data, err := os.ReadFile(filepath.Join(dir, "session_messages"))
			if err != nil || !strings.Contains(string(data), "original") || strings.Contains(string(data), "replacement") {
				t.Fatalf("messages=%s err=%v", data, err)
			}
		})
	}
}

func TestCommandUsageAndVersion(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "burrowtime", args: []string{"--version"}, want: "BurrowTime, version " + Version},
		{name: "watson", args: []string{"--version"}, want: "Watson, version " + WatsonVersion},
		{name: "help", want: "Available Commands:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &app{name: tc.name}
			root := a.root()
			root.SetArgs(tc.args)
			var out bytes.Buffer
			root.SetOut(&out)
			if err := root.Execute(); err != nil || !strings.Contains(out.String(), tc.want) {
				t.Fatalf("output=%q err=%v", out.String(), err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown flag", args: []string{"report", "--wat"}, want: "No such option: --wat"},
		{name: "missing argument", args: []string{"rename", "project"}, want: "Missing argument 'OLD_NAME'"},
		{name: "extra argument", args: []string{"remove", "x", "y"}, want: "Got unexpected extra argument (y)"},
		{name: "range extra", args: []string{"config", "a", "b", "c"}, want: "Got unexpected extra argument (c)"},
		{name: "required short option", args: []string{"add", "p"}, want: "Missing option '-f' / '--from'"},
		{name: "required long option", args: []string{"agent", "stop"}, want: "Missing option '-s' / '--session'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := (&app{name: "burrowtime"}).root()
			root.SetArgs(append([]string{"--data-dir", t.TempDir()}, tc.args...))
			cmd, err := root.ExecuteC()
			if err == nil || !isUsageFailure(err) || !strings.Contains(formatUsageFailure(cmd, err), tc.want) {
				t.Fatalf("err=%v formatted=%q", err, formatUsageFailure(cmd, err))
			}
		})
	}
	if FormatError(nil) != "" || FormatError(errors.New("Aborted!")) != "Aborted!" || FormatError(errors.New("failed")) != "Error: failed" || ExitCode(errors.New("failed")) != 1 {
		t.Fatal("runtime errors lost their formatting or status")
	}
}

func TestParseTimeSupportedInputs(t *testing.T) {
	t.Setenv("TZ", "UTC")
	now := time.Date(2026, 11, 4, 14, 30, 45, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "today", input: "today", want: "2026-11-04 00:00:00"},
		{name: "yesterday", input: "yesterday", want: "2026-11-03 00:00:00"},
		{name: "am", input: "9am", want: "2026-11-04 09:00:00"},
		{name: "pm with seconds", input: "yesterday 2:31:25 PM", want: "2026-11-03 14:31:25"},
		{name: "dated pm", input: "2020-01-02 2:30 PM", want: "2020-01-02 14:30:00"},
		{name: "today pm", input: "today 2 PM", want: "2026-11-04 14:00:00"},
		{name: "clock", input: "09:12:45", want: "2026-11-04 09:12:45"},
		{name: "date", input: "2020/1/2", want: "2020-01-02 00:00:00"},
		{name: "ordinal date", input: "2020-060", want: "2020-02-29 00:00:00"},
		{name: "year", input: "2020", want: "2020-01-01 00:00:00"},
		{name: "compact week day", input: "2020W014", want: "2020-01-02 00:00:00"},
		{name: "week day", input: "2020-W01-4", want: "2020-01-02 00:00:00"},
		{name: "compact week", input: "2020W01", want: "2019-12-30 00:00:00"},
		{name: "week", input: "2020-W01", want: "2019-12-30 00:00:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTime(tc.input, now)
			if err != nil || got.Format("2006-01-02 15:04:05") != tc.want {
				t.Fatalf("parseTime(%q)=%s err=%v, want %s", tc.input, got, err, tc.want)
			}
		})
	}
	for _, input := range []string{"13 PM", "2020-02-31 9 AM", "not-a-date", "2020-W01-9"} {
		t.Run(input, func(t *testing.T) {
			if _, err := parseTime(input, now); err == nil {
				t.Fatalf("accepted %q", input)
			}
		})
	}
	t.Setenv("TZ", "invalid/location")
	if _, err := parseTime("today", now); err == nil || !strings.Contains(err.Error(), "Invalid timezone") {
		t.Fatalf("timezone error=%v", err)
	}
}

func TestCLILifecycleAndValidation(t *testing.T) {
	t.Setenv("TZ", "UTC")
	dir := t.TempDir()
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "empty stop", args: []string{"stop"}, want: "No project started"},
		{name: "empty cancel", args: []string{"cancel"}, want: "No project started"},
		{name: "empty restart", args: []string{"restart"}, want: "No frames recorded"},
		{name: "missing project", args: []string{"start"}, want: "No project given"},
		{name: "concurrent stop", args: []string{"start", "p", "--concurrent", "--stop"}, want: "mutually exclusive"},
		{name: "concurrent no gap", args: []string{"start", "p", "--concurrent", "--no-gap"}, want: "mutually exclusive"},
		{name: "at no gap", args: []string{"start", "p", "--at", "09:00", "--no-gap"}, want: "mutually exclusive"},
		{name: "bad at", args: []string{"start", "p", "--at", "invalid"}, want: "Invalid time"},
		{name: "stop selectors", args: []string{"stop", "--all", "--timer", "primary"}, want: "mutually exclusive"},
		{name: "bad stop at", args: []string{"stop", "--at", "invalid"}, want: "Invalid time"},
		{name: "restart at gap", args: []string{"restart", "--at", "09:00", "--gap"}, want: "mutually exclusive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := runBurrowTimeCommand(dir, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("out=%q err=%v, want %q", out, err, tc.want)
			}
		})
	}
	steps := []struct {
		name  string
		args  []string
		input string
		want  string
	}{
		{name: "add", args: []string{"add", "alpha", "+tag", "--from", "2020-01-02 09:00", "--to", "2020-01-02 10:00"}, want: "Adding project alpha"},
		{name: "restart", args: []string{"restart", "--no-gap"}, want: "Starting project alpha"},
		{name: "status project", args: []string{"status", "--project"}, want: "alpha"},
		{name: "status tags", args: []string{"status", "--tags"}, want: "tag"},
		{name: "replace", args: []string{"restart", "--stop"}, want: "Starting project alpha"},
		{name: "cancel", args: []string{"cancel"}, want: "Canceling"},
		{name: "list projects", args: []string{"projects"}, want: "alpha"},
		{name: "list tags", args: []string{"tags"}, want: "tag"},
		{name: "frames", args: []string{"frames"}, want: ""},
		{name: "remove confirmed", args: []string{"remove", "--", "-1"}, input: "y\n", want: "Frame removed"},
	}
	for _, step := range steps {
		out, err := runBurrowTimeCommandInput(dir, step.input, step.args...)
		if err != nil || !strings.Contains(out, step.want) {
			t.Fatalf("%s: out=%q err=%v, want %q", step.name, out, err, step.want)
		}
	}
}

func TestStartJSONIncludesMessage(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		name := "primary"
		if concurrent {
			name = "concurrent"
		}
		t.Run(name, func(t *testing.T) {
			args := []string{"start", "project", "--json", "-m", "working on ticket"}
			if concurrent {
				args = append(args, "--concurrent")
			}
			out, err := runBurrowTimeCommand(t.TempDir(), args...)
			var timer struct {
				store.ActiveTimer
				Message string `json:"message"`
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(out), &timer); err != nil || timer.Message != "working on ticket" {
				t.Fatalf("timer=%+v err=%v output=%s", timer, err, out)
			}
		})
	}
}

func TestCommandReadFailuresDoNotWriteState(t *testing.T) {
	for _, command := range []string{"start", "stop", "status", "cancel", "restart", "add", "remove", "projects", "tags", "frames", "log", "report", "aggregate", "edit", "sync"} {
		t.Run(command, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "frames"), []byte("invalid JSON"), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{command}
			switch command {
			case "start":
				args = append(args, "project")
			case "add":
				args = append(args, "project", "--from", "2020-01-02", "--to", "2020-01-03")
			case "remove":
				args = append(args, "id")
			}
			if _, err := runBurrowTimeCommand(dir, args...); err == nil {
				t.Fatal("accepted corrupt frames")
			}
			if _, err := os.Stat(filepath.Join(dir, "state")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("wrote state after failed read: %v", err)
			}
		})
	}
}

func TestHumanizeAndStrftime(t *testing.T) {
	now := time.Date(2026, 11, 4, 14, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		ago  time.Duration
		want string
	}{
		{name: "now", want: "just now"},
		{name: "minute", ago: time.Minute, want: "a minute ago"},
		{name: "hour", ago: time.Hour, want: "an hour ago"},
		{name: "day", ago: 24 * time.Hour, want: "a day ago"},
		{name: "seconds", ago: 20 * time.Second, want: "20 seconds ago"},
		{name: "minutes", ago: 4 * time.Minute, want: "4 minutes ago"},
		{name: "hours", ago: 3 * time.Hour, want: "3 hours ago"},
		{name: "days", ago: 3 * 24 * time.Hour, want: "3 days ago"},
		{name: "week", ago: 8 * 24 * time.Hour, want: "a week ago"},
		{name: "weeks", ago: 14 * 24 * time.Hour, want: "2 weeks ago"},
		{name: "month", ago: 21 * 24 * time.Hour, want: "a month ago"},
		{name: "months", ago: 75 * 24 * time.Hour, want: "3 months ago"},
		{name: "year", ago: 365 * 24 * time.Hour, want: "a year ago"},
		{name: "years", ago: 3 * 365 * 24 * time.Hour, want: "3 years ago"},
		{name: "future", ago: -3 * time.Minute, want: "in 3 minutes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := humanize(now.Add(-tc.ago), now); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
	if got := pythonStrftime(now, "%Y-%m-%d %H:%M:%S %%"); got != "2026-11-04 14:30:00 %" {
		t.Fatalf("formatted=%q", got)
	}
	for _, tc := range []struct {
		name   string
		format string
		want   string
	}{
		{name: "no padding", format: "%-d %-H", want: "4 14"},
		{name: "space padding", format: "%_d", want: " 4"},
		{name: "zero padding", format: "%0e", want: "04"},
		{name: "uppercase", format: "%^B", want: "NOVEMBER"},
		{name: "swap case", format: "%#B", want: "nOVEMBER"},
		{name: "unknown", format: "%Q %_Q %", want: "%Q %_Q %"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pythonStrftime(now, tc.format); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
	if got := formatDuration(-3661); got != "-1h 01m 01s" {
		t.Fatalf("duration=%s", got)
	}
	if got := shortID("abc"); got != "abc" {
		t.Fatal(got)
	}
}
