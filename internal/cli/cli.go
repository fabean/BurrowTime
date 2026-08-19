package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/fabean/BurrowTime/internal/store"
	"github.com/fabean/BurrowTime/internal/watson"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

var Version = "dev"

const WatsonVersion = "2.1.0"

type app struct {
	dir                    string
	name                   string
	in                     *os.File
	out, errOut            *os.File
	colorFlag, noColorFlag bool
}

func Execute(args []string) error {
	name := filepath.Base(os.Args[0])
	if name != "watson" {
		name = "burrowtime"
	}
	a := &app{name: name, in: os.Stdin, out: os.Stdout, errOut: os.Stderr}
	if name == "burrowtime" && len(args) == 0 && term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("BURROWTIME_TUI") != "0" {
		dir, err := a.resolveDir()
		if err != nil {
			return err
		}
		a.dir = dir
		if err := a.offerWatsonMigration(os.Stdin, os.Stdout, true); err != nil {
			return err
		}
		args, err = runDashboard(a.dir)
		if err != nil {
			return err
		}
		if len(args) == 0 {
			return nil
		}
	}
	root := a.root()
	if name == "watson" {
		if command, flag := misplacedWatsonGlobal(root, args); command != nil {
			err := fmt.Errorf("unknown flag: %s", flag)
			return &exitError{code: 2, message: formatUsageFailure(command, err)}
		}
	}
	normalized := normalizeNegativeFrameArgs(args)
	root.SetArgs(normalizeIgnoredOptions(root, normalized))
	root.SetIn(a.in)
	root.SetOut(a.out)
	root.SetErr(a.errOut)
	command, err := root.ExecuteC()
	if err != nil && isUsageFailure(err) {
		return &exitError{code: 2, message: formatUsageFailure(command, err)}
	}
	return err
}

func misplacedWatsonGlobal(root *cobra.Command, args []string) (*cobra.Command, string) {
	commandIndex := -1
	command := root
	for i, value := range args {
		if value == "-v" {
			return command, value
		}
		if commandIndex < 0 && !strings.HasPrefix(value, "-") {
			for _, candidate := range root.Commands() {
				if candidate.Name() == value {
					commandIndex, command = i, candidate
					break
				}
			}
			continue
		}
		if commandIndex >= 0 && (value == "--color" || value == "--no-color") {
			return command, value
		}
	}
	return nil, ""
}

type exitError struct {
	code    int
	message string
}

func (e *exitError) Error() string { return e.message }

// ExitCode returns Click-compatible process status codes. Runtime errors use
// 1; command-line usage errors use 2.
func ExitCode(err error) int {
	var typed *exitError
	if errors.As(err, &typed) {
		return typed.code
	}
	return 1
}

func FormatError(err error) string {
	if err == nil {
		return ""
	}
	if err.Error() == "Aborted!" {
		return "Aborted!"
	}
	var typed *exitError
	if errors.As(err, &typed) {
		return typed.message
	}
	return "Error: " + err.Error()
}

var (
	unknownCommandPattern = regexp.MustCompile(`(?s)^unknown command "([^"]+)" for "([^"]+)".*$`)
	receivedArgsPattern   = regexp.MustCompile(`^accepts (?:at most )?(\d+) arg\(s\), received (\d+)$`)
	rangeArgsPattern      = regexp.MustCompile(`^accepts between (\d+) and (\d+) arg\(s\), received (\d+)$`)
	requiredFlagPattern   = regexp.MustCompile(`^required flag\(s\) "([^"]+)"`)
)

func isUsageFailure(err error) bool {
	message := err.Error()
	return unknownCommandPattern.MatchString(message) ||
		strings.HasPrefix(message, "unknown flag: ") ||
		strings.HasPrefix(message, "accepts ") ||
		requiredFlagPattern.MatchString(message) ||
		strings.HasPrefix(message, "Could not match value '") ||
		strings.HasPrefix(message, "Invalid value for '")
}

func clickUsage(command *cobra.Command) string {
	path := command.CommandPath()
	suffix := "[OPTIONS]"
	switch command.Name() {
	case "watson", "burrowtime":
		suffix = "[OPTIONS] COMMAND [ARGS]..."
	case "start", "add":
		suffix = "[OPTIONS] [ARGS]..."
	case "config":
		suffix = "[OPTIONS] SECTION.OPTION [VALUE]"
	case "edit", "restart":
		suffix = "[OPTIONS] [ID]"
	case "merge":
		suffix = "[OPTIONS] FRAMES_WITH_CONFLICT"
	case "remove":
		suffix = "[OPTIONS] ID"
	case "rename":
		suffix = "[OPTIONS] TYPE OLD_NAME NEW_NAME"
	}
	return "Usage: " + path + " " + suffix
}

func formatUsageFailure(command *cobra.Command, err error) string {
	message := err.Error()
	if match := unknownCommandPattern.FindStringSubmatch(message); match != nil {
		if command.HasSubCommands() {
			message = fmt.Sprintf("No such command '%s'.", match[1])
			var candidates []string
			for _, child := range command.Commands() {
				if child.Name() != "completion" {
					candidates = append(candidates, child.Name())
				}
			}
			if matches := closeMatches(match[1], candidates, 3, 0.5); len(matches) > 0 {
				message += "\n\nDid you mean one of these?"
				for _, candidate := range matches {
					message += "\n    " + candidate
				}
			}
		} else {
			message = fmt.Sprintf("Got unexpected extra argument (%s)", match[1])
		}
	} else if strings.HasPrefix(message, "unknown flag: ") {
		unknown := strings.TrimPrefix(message, "unknown flag: ")
		message = "No such option: " + unknown
		var candidates []string
		command.LocalNonPersistentFlags().VisitAll(func(flag *pflag.Flag) {
			if !flag.Hidden {
				candidates = append(candidates, "--"+flag.Name)
			}
		})
		if matches := closeMatches(unknown, candidates, 3, 0.6); len(matches) == 1 {
			message += " Did you mean " + matches[0] + "?"
		} else if len(matches) > 1 {
			sort.Strings(matches)
			message += " (Possible options: " + strings.Join(matches, ", ") + ")"
		}
	} else if match := receivedArgsPattern.FindStringSubmatch(message); match != nil {
		expected, _ := strconv.Atoi(match[1])
		received, _ := strconv.Atoi(match[2])
		names := map[string][]string{
			"merge":  {"FRAMES_WITH_CONFLICT"},
			"remove": {"ID"},
			"rename": {"TYPE", "OLD_NAME", "NEW_NAME"},
		}
		if received < expected && received < len(names[command.Name()]) {
			message = fmt.Sprintf("Missing argument '%s'.", names[command.Name()][received])
		} else {
			args := command.Flags().Args()
			if expected < len(args) {
				message = fmt.Sprintf("Got unexpected extra argument (%s)", args[expected])
			}
		}
	} else if match := rangeArgsPattern.FindStringSubmatch(message); match != nil {
		maximum, _ := strconv.Atoi(match[2])
		args := command.Flags().Args()
		if maximum < len(args) {
			message = fmt.Sprintf("Got unexpected extra argument (%s)", args[maximum])
		}
	} else if match := requiredFlagPattern.FindStringSubmatch(message); match != nil {
		flag := command.Flags().Lookup(match[1])
		if flag != nil && flag.Shorthand != "" {
			message = fmt.Sprintf("Missing option '-%s' / '--%s'.", flag.Shorthand, flag.Name)
		} else {
			message = fmt.Sprintf("Missing option '--%s'.", match[1])
		}
	}
	return fmt.Sprintf("%s\nTry '%s --help' for help.\n\nError: %s", clickUsage(command), command.CommandPath(), message)
}

// Click's ignore_unknown_options mode preserves unknown dash-prefixed tokens
// as positional arguments while continuing to parse recognized options that
// follow. pflag's whitelist discards those tokens, so reorder only these five
// compatible commands into known options followed by their original
// positional stream.
func normalizeIgnoredOptions(root *cobra.Command, args []string) []string {
	commandIndex := -1
	for i := 0; i < len(args); i++ {
		value := args[i]
		if value == "--watson-dir" {
			i++
			continue
		}
		if strings.HasPrefix(value, "--watson-dir=") || value == "--color" || value == "--no-color" {
			continue
		}
		if !strings.HasPrefix(value, "-") {
			commandIndex = i
			break
		}
	}
	if commandIndex < 0 {
		return args
	}
	ignored := map[string]bool{"add": true, "edit": true, "remove": true, "restart": true, "stop": true}
	name := args[commandIndex]
	if !ignored[name] {
		return args
	}
	var command *cobra.Command
	for _, candidate := range root.Commands() {
		if candidate.Name() == name {
			command = candidate
			break
		}
	}
	if command == nil {
		return args
	}
	command.InheritedFlags()
	known := append([]string(nil), args[:commandIndex+1]...)
	positionals := []string{}
	for i := commandIndex + 1; i < len(args); i++ {
		value := args[i]
		if value == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if value == "--help" || value == "-h" {
			known = append(known, value)
			continue
		}
		var flag *pflag.Flag
		hasAttachedValue := false
		if strings.HasPrefix(value, "--") && len(value) > 2 {
			name := strings.TrimPrefix(value, "--")
			if before, _, found := strings.Cut(name, "="); found {
				name, hasAttachedValue = before, true
			}
			flag = command.Flags().Lookup(name)
		} else if strings.HasPrefix(value, "-") && len(value) > 1 {
			flag = command.Flags().ShorthandLookup(value[1:2])
			hasAttachedValue = len(value) > 2
		}
		if flag == nil {
			positionals = append(positionals, value)
			continue
		}
		known = append(known, value)
		if flag.NoOptDefVal == "" && !hasAttachedValue && i+1 < len(args) {
			i++
			known = append(known, args[i])
		}
	}
	if len(positionals) > 0 {
		known = append(known, "--")
		known = append(known, positionals...)
	}
	return known
}

type similarity struct {
	name  string
	score float64
}

// closeMatches mirrors the small difflib.get_close_matches surface Click and
// click-didyoumean use for option and command suggestions.
func closeMatches(word string, possibilities []string, limit int, cutoff float64) []string {
	matches := make([]similarity, 0, len(possibilities))
	for _, possibility := range possibilities {
		// difflib fixes the misspelled word as seq2, which matters because
		// SequenceMatcher's tie-breaking is intentionally asymmetric.
		score := gestaltRatio(possibility, word)
		if score >= cutoff {
			matches = append(matches, similarity{name: possibility, score: score})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].name > matches[j].name
		}
		return matches[i].score > matches[j].score
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	result := make([]string, len(matches))
	for i := range matches {
		result[i] = matches[i].name
	}
	return result
}

func gestaltRatio(a, b string) float64 {
	ar, br := []rune(a), []rune(b)
	if len(ar)+len(br) == 0 {
		return 1
	}
	var matched func(int, int, int, int) int
	matched = func(a0, a1, b0, b1 int) int {
		bestA, bestB, bestSize := a0, b0, 0
		for i := a0; i < a1; i++ {
			for j := b0; j < b1; j++ {
				size := 0
				for i+size < a1 && j+size < b1 && ar[i+size] == br[j+size] {
					size++
				}
				if size > bestSize {
					bestA, bestB, bestSize = i, j, size
				}
			}
		}
		if bestSize == 0 {
			return 0
		}
		return bestSize + matched(a0, bestA, b0, bestB) + matched(bestA+bestSize, a1, bestB+bestSize, b1)
	}
	return 2 * float64(matched(0, len(ar), 0, len(br))) / float64(len(ar)+len(br))
}

func normalizeNegativeFrameArgs(args []string) []string {
	commandIndex := -1
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--watson-dir" || arg == "--data-dir" {
			i++
			continue
		}
		if strings.HasPrefix(arg, "--watson-dir=") || strings.HasPrefix(arg, "--data-dir=") || arg == "--color" || arg == "--no-color" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		commandIndex = i
		break
	}
	if commandIndex < 0 || (args[commandIndex] != "edit" && args[commandIndex] != "remove" && args[commandIndex] != "restart") {
		return args
	}
	index := -1
	for i := commandIndex + 1; i < len(args); i++ {
		value := args[i]
		if len(value) > 1 && value[0] == '-' {
			if _, err := strconv.Atoi(value); err == nil {
				index = i
				break
			}
		}
	}
	if index < 0 {
		return args
	}
	normalized := append([]string(nil), args[:index]...)
	normalized = append(normalized, args[index+1:]...)
	normalized = append(normalized, "--", args[index])
	return normalized
}

func (a *app) resolveDir() (string, error) {
	if a.dir != "" {
		return a.dir, nil
	}
	if a.name == "watson" {
		return store.WatsonDir()
	}
	return store.BurrowTimeDir()
}

func (a *app) open() (*watson.Service, error) {
	dir, err := a.resolveDir()
	if err != nil {
		return nil, err
	}
	return watson.Open(dir)
}

func (a *app) openData() (*watson.Service, error) {
	dir, err := a.resolveDir()
	if err != nil {
		return nil, err
	}
	return watson.OpenData(dir)
}

func (a *app) root() *cobra.Command {
	if a.name == "" {
		a.name = "burrowtime"
	}
	root := &cobra.Command{
		Use: a.name, Short: "Watson-compatible time tracking in Go",
		Long:         "BurrowTime tracks your time with Watson-compatible commands and its own local data directory.",
		SilenceUsage: true, SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			stylingEnabled = a.colorFlag
			if !a.colorFlag && !a.noColorFlag {
				if file, ok := cmd.OutOrStdout().(*os.File); ok {
					stylingEnabled = term.IsTerminal(int(file.Fd()))
				}
			}
			if a.name == "burrowtime" && cmd.CommandPath() != "burrowtime migrate" && cmd.Parent() != nil && cmd.Parent().Name() != "migrate" {
				customDir := cmd.Flags().Changed("data-dir") || cmd.Flags().Changed("watson-dir")
				if !customDir {
					in, inOK := cmd.InOrStdin().(*os.File)
					out, outOK := cmd.OutOrStdout().(*os.File)
					interactive := inOK && outOK && term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd()))
					if err := a.offerWatsonMigration(cmd.InOrStdin(), cmd.OutOrStdout(), interactive); err != nil {
						return err
					}
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	if a.name == "watson" {
		root.Version = WatsonVersion
		root.SetVersionTemplate("Watson, version {{.Version}}\n")
	} else {
		root.Version = Version
		root.SetVersionTemplate("BurrowTime, version {{.Version}}\n")
	}
	root.InitDefaultVersionFlag()
	root.Flags().Lookup("version").Shorthand = ""
	if a.name == "watson" {
		root.PersistentFlags().StringVar(&a.dir, "watson-dir", "", "Watson data directory (defaults to WATSON_DIR)")
	} else {
		root.PersistentFlags().StringVar(&a.dir, "data-dir", "", "BurrowTime data directory (defaults to BURROWTIME_DIR)")
		root.PersistentFlags().StringVar(&a.dir, "watson-dir", "", "deprecated alias for --data-dir")
		_ = root.PersistentFlags().MarkHidden("watson-dir")
	}
	root.PersistentFlags().BoolFunc("color", "force color output", func(value string) error {
		enabled, err := strconv.ParseBool(value)
		if err == nil && enabled {
			a.colorFlag, a.noColorFlag = true, false
		}
		return err
	})
	root.PersistentFlags().BoolFunc("no-color", "disable color output", func(value string) error {
		enabled, err := strconv.ParseBool(value)
		if err == nil && enabled {
			a.colorFlag, a.noColorFlag = false, true
		}
		return err
	})
	if a.name == "watson" {
		_ = root.PersistentFlags().MarkHidden("watson-dir")
	}
	root.AddCommand(a.start(), a.stop(), a.status(), a.cancel(), a.restart(), a.add(), a.remove(), a.projects(), a.tags(), a.frames())
	root.AddCommand(a.log(), a.report(), a.aggregate())
	root.AddCommand(a.rename(), a.config())
	root.AddCommand(a.edit(), a.merge(), a.sync())
	if a.name != "watson" {
		root.AddCommand(a.migrate())
	}
	return root
}

func parseProjectTags(args []string) (string, []string) {
	projectParts := []string{}
	tags := []string{}
	i := 0
	for i < len(args) && !strings.HasPrefix(args[i], "+") {
		projectParts = append(projectParts, args[i])
		i++
	}
	for i < len(args) {
		if !strings.HasPrefix(args[i], "+") {
			i++
			continue
		}
		parts := []string{strings.TrimPrefix(args[i], "+")}
		i++
		for i < len(args) && !strings.HasPrefix(args[i], "+") {
			parts = append(parts, args[i])
			i++
		}
		if tag := strings.TrimSpace(strings.Join(parts, " ")); tag != "" {
			tags = append(tags, tag)
		}
	}
	return strings.Join(projectParts, " "), tags
}

func parseTime(value string, now time.Time) (time.Time, error) {
	loc := now.Location()
	if tz := os.Getenv("TZ"); tz != "" {
		loaded, err := time.LoadLocation(tz)
		if err != nil {
			return time.Time{}, fmt.Errorf("Invalid timezone %s specified, please set the TZ environment variable with a valid timezone.", tz)
		}
		loc = loaded
		now = now.In(loc)
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05Z07:00", "2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999", "2006-1-2 15:04:05", "2006-1-2T15:04:05", "2006-1-2 15:04", "2006-1-2T15:04", "2006-1-2 15", "2006-1-2T15", "2006-1-2", "2006/1/2", "2006.1.2", "20060102", "2006-002", "2006-01", "2006"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return localWallTime(parsed.Year(), parsed.Month(), parsed.Day(), parsed.Hour(), parsed.Minute(), parsed.Second(), parsed.Nanosecond(), loc), nil
		}
	}
	if len(value) == 8 && value[4] == 'W' {
		if year, e := strconv.Atoi(value[:4]); e == nil {
			if week, e := strconv.Atoi(value[5:7]); e == nil {
				weekday := 1
				if len(value) == 8 {
					weekday = int(value[7] - '0')
				}
				if weekday >= 1 && weekday <= 7 {
					return isoWeekDate(year, week, weekday, loc), nil
				}
			}
		}
	}
	if len(value) == 10 && value[4] == '-' && value[5] == 'W' && value[8] == '-' {
		if year, e := strconv.Atoi(value[:4]); e == nil {
			if week, e := strconv.Atoi(value[6:8]); e == nil {
				weekday := int(value[9] - '0')
				if weekday >= 1 && weekday <= 7 {
					return isoWeekDate(year, week, weekday, loc), nil
				}
			}
		}
	}
	if len(value) == 7 && value[4] == 'W' {
		if year, e := strconv.Atoi(value[:4]); e == nil {
			if week, e := strconv.Atoi(value[5:]); e == nil {
				return isoWeekDate(year, week, 1, loc), nil
			}
		}
	}
	if len(value) == 8 && value[4] == '-' && value[5] == 'W' {
		if year, e := strconv.Atoi(value[:4]); e == nil {
			if week, e := strconv.Atoi(value[6:]); e == nil {
				return isoWeekDate(year, week, 1, loc), nil
			}
		}
	}
	for _, layout := range []string{"15:04:05", "15:04"} {
		if parsed, err := time.ParseInLocation(layout, value, loc); err == nil {
			return localWallTime(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), parsed.Second(), 0, loc), nil
		}
	}
	return time.Time{}, fmt.Errorf("Could not match value '%s' to any supported date format", value)
}

// dateutil assigns the post-transition offset to nonexistent local times,
// retaining the requested wall clock. time.Date chooses an equivalent instant
// using the pre-transition representation instead. Re-wrap that instant in a
// fixed post-transition zone to preserve both Watson's timestamp and display.
func localWallTime(year int, month time.Month, day, hour, minute, second, nanosecond int, loc *time.Location) time.Time {
	candidate := time.Date(year, month, day, hour, minute, second, nanosecond, loc)
	cy, cm, cd := candidate.Date()
	if cy == year && cm == month && cd == day && candidate.Hour() == hour && candidate.Minute() == minute && candidate.Second() == second {
		return candidate
	}
	_, initialOffset := candidate.Zone()
	for step := 1; step <= 12; step++ {
		probe := candidate.Add(time.Duration(step) * 30 * time.Minute)
		name, offset := probe.Zone()
		if offset != initialOffset {
			return time.Date(year, month, day, hour, minute, second, nanosecond, time.FixedZone(name, offset))
		}
	}
	return candidate
}

func isoWeekDate(year, week, weekday int, loc *time.Location) time.Time {
	jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, loc)
	monday := jan4.AddDate(0, 0, -((int(jan4.Weekday()) + 6) % 7))
	return monday.AddDate(0, 0, (week-1)*7+weekday-1)
}

func (a *app) start() *cobra.Command {
	var at string
	gap := true
	var stopSet, stopRunning, concurrent bool
	var confirmProject, confirmTag bool
	cmd := &cobra.Command{Use: "start [project] [+tag ...]", Short: "Start monitoring time for the given project.", Args: cobra.ArbitraryArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if at != "" && (cmd.Flags().Changed("gap") || cmd.Flags().Changed("no-gap")) {
			return errors.New("The following options are mutually exclusive: `--at`, `--gap_`")
		}
		if concurrent && stopSet && stopRunning {
			return errors.New("--concurrent and --stop are mutually exclusive")
		}
		if concurrent && !gap {
			return errors.New("--concurrent and --no-gap are mutually exclusive")
		}
		s, err := a.open()
		if err != nil {
			return err
		}
		project, tags := parseProjectTags(args)
		if project == "" {
			return errors.New("No project given.")
		}
		var when *time.Time
		if at != "" {
			parsed, e := parseTime(at, s.Now())
			if e != nil {
				return e
			}
			when = &parsed
		}
		reader := bufio.NewReader(cmd.InOrStdin())
		if s.Config.Bool("options", "confirm_new_project", false) || confirmProject {
			if err := confirmNewProject(reader, cmd, project, s.Projects()); err != nil {
				return err
			}
		}
		if s.Config.Bool("options", "confirm_new_tag", false) || confirmTag {
			if err := confirmNewTags(reader, cmd, tags, s.Tags()); err != nil {
				return err
			}
		}
		timers := s.RunningTimers()
		if len(timers) > 0 && !gap {
			return fmt.Errorf("Project '%s' is already started and '--no-gap' is passed. Please stop manually.", timers[0].Project)
		}
		stopOnStart := s.Config.Bool("options", "stop_on_start", false)
		if len(timers) > 0 && !concurrent && !stopSet && !stopOnStart && a.name != "watson" && commandSupportsTUI(cmd) {
			choice, pickErr := runStartConflictPicker(timers, project, tags, s.Now())
			if pickErr != nil {
				return pickErr
			}
			switch choice {
			case startConflictReplace:
				stopSet, stopRunning = true, true
			case startConflictConcurrent:
				concurrent = true
			}
		}
		if concurrent {
			timer, startErr := s.StartConcurrent(project, tags, when)
			if startErr != nil {
				return startErr
			}
			displayStart := time.Unix(timer.Start, 0).In(s.Now().Location())
			if when != nil {
				displayStart = *when
			}
			if len(timers) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Starting project %s%s at %s\n", styleText("project", timer.Project), formatTags(timer.Tags), styleText("time", displayStart.Format("15:04")))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Starting concurrent project %s%s at %s (timer: %s)\n", styleText("project", timer.Project), formatTags(timer.Tags), styleText("time", displayStart.Format("15:04")), styleText("id", shortID(timer.ID)))
			}
			return nil
		}
		if len(timers) > 0 {
			shouldStop := stopOnStart
			if stopSet {
				shouldStop = stopRunning
			}
			if !shouldStop {
				return fmt.Errorf("Project %s is already started.", timers[0].Project)
			}
			if a.name == "watson" {
				if _, err = s.Stop(when); err != nil {
					return err
				}
			} else {
				stopped, stopErr := s.StopAll(when)
				if stopErr != nil {
					return stopErr
				}
				if stopSet && stopRunning {
					fmt.Fprintf(cmd.OutOrStdout(), "Stopped %d running %s.\n", len(stopped), plural(len(stopped), "timer", "timers"))
				}
			}
		}
		state, err := s.Start(project, tags, when, gap, false)
		if err != nil {
			return err
		}
		displayStart := time.Unix(state.Start, 0).In(s.Now().Location())
		if when != nil {
			displayStart = *when
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Starting project %s%s at %s\n", styleText("project", state.Project), formatTags(state.Tags), styleText("time", displayStart.Format("15:04")))
		return nil
	}}
	cmd.Flags().StringVar(&at, "at", "", "start frame at this time")
	cmd.Flags().BoolFuncP("gap", "g", "leave a gap after the previous frame", func(value string) error {
		enabled, err := strconv.ParseBool(value)
		if err == nil {
			gap = enabled
		}
		return err
	})
	cmd.Flags().BoolFuncP("no-gap", "G", "do not leave a gap after the previous frame", func(value string) error {
		enabled, err := strconv.ParseBool(value)
		if err == nil {
			gap = !enabled
		}
		return err
	})
	cmd.Flags().BoolVarP(&confirmProject, "confirm-new-project", "c", false, "confirm addition of new project")
	cmd.Flags().BoolVarP(&confirmTag, "confirm-new-tag", "b", false, "confirm creation of new tag")
	if a.name != "watson" {
		cmd.Flags().BoolFuncP("stop", "s", "stop all running timers before starting", func(value string) error {
			enabled, err := strconv.ParseBool(value)
			if err == nil && enabled {
				stopSet, stopRunning = true, true
			}
			return err
		})
		cmd.Flags().BoolFuncP("no-stop", "S", "do not stop running timers", func(value string) error {
			enabled, err := strconv.ParseBool(value)
			if err == nil && enabled {
				stopSet, stopRunning = true, false
			}
			return err
		})
		cmd.Flags().BoolVar(&concurrent, "concurrent", false, "start another timer concurrently")
	}
	cmd.ValidArgsFunction = a.completeProjectOrTag
	return cmd
}

func commandSupportsTUI(cmd *cobra.Command) bool {
	if os.Getenv("BURROWTIME_TUI") == "0" {
		return false
	}
	in, inOK := cmd.InOrStdin().(*os.File)
	out, outOK := cmd.OutOrStdout().(*os.File)
	return inOK && outOK && term.IsTerminal(int(in.Fd())) && term.IsTerminal(int(out.Fd()))
}

func (a *app) stop() *cobra.Command {
	var at string
	var all bool
	var timerRef string
	cmd := &cobra.Command{Use: "stop", Short: "Stop monitoring time for the current project.", Args: cobra.NoArgs, FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true}, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := a.openData()
		if err != nil {
			return err
		}
		var when *time.Time
		if at != "" {
			parsed, e := parseTime(at, s.Now())
			if e != nil {
				return e
			}
			when = &parsed
		}
		if all && timerRef != "" {
			return errors.New("--all and --timer are mutually exclusive")
		}
		if all {
			frames, stopErr := s.StopAll(when)
			if stopErr != nil {
				return stopErr
			}
			for _, frame := range frames {
				printStoppedFrame(cmd, frame, s.Now())
			}
			return nil
		}
		if timerRef != "" {
			frame, stopErr := s.StopTimer(timerRef, when)
			if stopErr != nil {
				return stopErr
			}
			printStoppedFrame(cmd, frame, s.Now())
			return nil
		}
		timers := s.RunningTimers()
		if len(timers) == 0 {
			return errors.New("No project started.")
		}
		if len(timers) == 1 || a.name == "watson" {
			frame, stopErr := s.StopTimer(timers[0].ID, when)
			if stopErr != nil {
				return stopErr
			}
			printStoppedFrame(cmd, frame, s.Now())
			return nil
		}
		in, inOK := cmd.InOrStdin().(*os.File)
		out, outOK := cmd.OutOrStdout().(*os.File)
		if !inOK || !outOK || !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
			return errors.New("Multiple timers are running; pass --timer <id> or --all.")
		}
		selection, pickErr := runStopPicker(timers, s.Now())
		if pickErr != nil {
			return pickErr
		}
		if selection == stopAllSelection {
			frames, stopErr := s.StopAll(when)
			if stopErr != nil {
				return stopErr
			}
			for _, frame := range frames {
				printStoppedFrame(cmd, frame, s.Now())
			}
			return nil
		}
		frame, stopErr := s.StopTimer(selection, when)
		if stopErr != nil {
			return stopErr
		}
		printStoppedFrame(cmd, frame, s.Now())
		return nil
	}}
	cmd.Flags().StringVar(&at, "at", "", "stop frame at this time")
	if a.name != "watson" {
		cmd.Flags().BoolVarP(&all, "all", "a", false, "stop all running timers")
		cmd.Flags().StringVarP(&timerRef, "timer", "t", "", "stop a running timer by ID")
		_ = cmd.RegisterFlagCompletionFunc("timer", a.completeTimers)
	}
	return cmd
}

func printStoppedFrame(cmd *cobra.Command, frame store.Frame, now time.Time) {
	fmt.Fprintf(cmd.OutOrStdout(), "Stopping project %s%s, started %s and stopped %s. (id: %s)\n", styleText("project", frame.Project), formatTags(frame.Tags), styleText("time", humanize(time.Unix(frame.Start, 0), now)), styleText("time", humanize(time.Unix(*frame.Stop, 0), now)), styleText("id", shortID(frame.ID)))
}

func plural(count int, singular, multiple string) string {
	if count == 1 {
		return singular
	}
	return multiple
}

func (a *app) status() *cobra.Command {
	var project, tags, elapsed bool
	cmd := &cobra.Command{Use: "status", Short: "Display the current project and elapsed time.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := a.openData()
		if err != nil {
			return err
		}
		timers := s.RunningTimers()
		if len(timers) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No project started.")
			return nil
		}
		if project {
			for _, timer := range timers {
				fmt.Fprintln(cmd.OutOrStdout(), styleText("project", timer.Project))
			}
			return nil
		}
		if tags {
			for _, timer := range timers {
				fmt.Fprintln(cmd.OutOrStdout(), strings.Trim(formatTags(timer.Tags), " "))
			}
			return nil
		}
		if elapsed {
			for _, timer := range timers {
				fmt.Fprintln(cmd.OutOrStdout(), styleText("time", humanize(time.Unix(timer.Start, 0), s.Now())))
			}
			return nil
		}
		if err := s.LoadConfig(); err != nil {
			return err
		}
		dateFmt := s.Config.Get("options", "date_format", "%Y.%m.%d")
		timeFmt := s.Config.Get("options", "time_format", "%H:%M:%S%z")
		if len(timers) > 1 {
			fmt.Fprintf(cmd.OutOrStdout(), "%d timers running:\n", len(timers))
		}
		for _, timer := range timers {
			started := time.Unix(timer.Start, 0).In(watsonDisplayLocation(s.Now()))
			prefix := ""
			if len(timers) > 1 {
				prefix = "  " + styleText("id", fmt.Sprintf("%-7s", shortID(timer.ID))) + "  "
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%sProject %s%s started %s (%s %s)\n", prefix, styleText("project", timer.Project), formatTags(timer.Tags), styleText("time", humanize(started, s.Now())), styleText("date", pythonStrftime(started, dateFmt)), styleText("time", pythonStrftime(started, timeFmt)))
		}
		return nil
	}}
	cmd.Flags().BoolVarP(&project, "project", "p", false, "only output project")
	cmd.Flags().BoolVarP(&tags, "tags", "t", false, "only show tags")
	cmd.Flags().BoolVarP(&elapsed, "elapsed", "e", false, "only show time elapsed")
	return cmd
}

func (a *app) cancel() *cobra.Command {
	return &cobra.Command{Use: "cancel", Short: "Cancel the last call to start.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, e := a.openData()
		if e != nil {
			return e
		}
		old, e := s.Cancel()
		if e != nil {
			return e
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Canceling the timer for project %s%s\n", styleText("project", old.Project), formatTags(old.Tags))
		return nil
	}}
}

func (a *app) restart() *cobra.Command {
	var at string
	gap := true
	var stopSet, stopRunning bool
	cmd := &cobra.Command{Use: "restart [id]", Short: "Restart a previously stopped project.", Args: cobra.MaximumNArgs(1), FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true}, RunE: func(cmd *cobra.Command, args []string) error {
		if at != "" && (cmd.Flags().Changed("gap") || cmd.Flags().Changed("no-gap")) {
			return errors.New("The following options are mutually exclusive: `--at`, `--gap_`")
		}
		s, e := a.openData()
		if e != nil {
			return e
		}
		ref := "-1"
		if len(args) > 0 {
			ref = args[0]
		}
		var when *time.Time
		if at != "" {
			parsed, err := parseTime(at, s.Now())
			if err != nil {
				return err
			}
			when = &parsed
		}
		timers := s.RunningTimers()
		if len(s.Frames) == 0 && len(timers) == 0 {
			return errors.New("No frames recorded yet. It's time to create your first one!")
		}
		if len(timers) > 0 && !gap {
			return fmt.Errorf("Project '%s' is already started and '--no-gap' is passed. Please stop manually.", timers[0].Project)
		}
		if len(timers) > 0 {
			if !stopSet {
				if e = s.LoadConfig(); e != nil {
					return e
				}
			}
			if (stopSet && stopRunning) || (!stopSet && s.Config.Bool("options", "stop_on_restart", false)) {
				if a.name == "watson" {
					if _, e = s.Stop(nil); e != nil {
						return e
					}
				} else if _, e = s.StopAll(nil); e != nil {
					return e
				}
			} else {
				return fmt.Errorf("Project already started: %s%s", timers[0].Project, formatTags(timers[0].Tags))
			}
		}
		f, _, e := s.Lookup(ref)
		if e != nil {
			return e
		}
		state, e := s.Start(f.Project, f.Tags, when, gap, true)
		if e != nil {
			return e
		}
		displayStart := time.Unix(state.Start, 0).In(s.Now().Location())
		if when != nil {
			displayStart = *when
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Starting project %s%s at %s\n", styleText("project", state.Project), formatTags(state.Tags), styleText("time", displayStart.Format("15:04")))
		return nil
	}}
	cmd.Flags().StringVar(&at, "at", "", "start frame at this time")
	cmd.Flags().BoolFuncP("gap", "g", "leave a gap", func(value string) error {
		enabled, err := strconv.ParseBool(value)
		if err == nil {
			gap = enabled
		}
		return err
	})
	cmd.Flags().BoolFuncP("no-gap", "G", "do not leave a gap", func(value string) error {
		enabled, err := strconv.ParseBool(value)
		if err == nil {
			gap = !enabled
		}
		return err
	})
	cmd.Flags().BoolFuncP("stop", "s", "stop an already running project", func(value string) error {
		enabled, err := strconv.ParseBool(value)
		if err == nil {
			stopSet, stopRunning = true, enabled
		}
		return err
	})
	cmd.Flags().BoolFuncP("no-stop", "S", "do not stop an already running project", func(value string) error {
		enabled, err := strconv.ParseBool(value)
		if err == nil {
			stopSet, stopRunning = true, !enabled
		}
		return err
	})
	cmd.ValidArgsFunction = a.completeFrames
	return cmd
}

func (a *app) add() *cobra.Command {
	var from, to string
	var confirmProject, confirmTag bool
	cmd := &cobra.Command{Use: "add [project] [+tag ...]", Short: "Add time not tracked live.", Args: cobra.ArbitraryArgs, FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true}, RunE: func(cmd *cobra.Command, args []string) error {
		s, e := a.open()
		if e != nil {
			return e
		}
		p, t := parseProjectTags(args)
		if p == "" {
			return errors.New("No project given.")
		}
		f, e1 := parseTime(from, s.Now())
		if e1 != nil {
			return e1
		}
		t2, e2 := parseTime(to, s.Now())
		if e2 != nil {
			return e2
		}
		reader := bufio.NewReader(cmd.InOrStdin())
		if s.Config.Bool("options", "confirm_new_project", false) || confirmProject {
			if err := confirmNewProject(reader, cmd, p, s.Projects()); err != nil {
				return err
			}
		}
		if s.Config.Bool("options", "confirm_new_tag", false) || confirmTag {
			if err := confirmNewTags(reader, cmd, t, s.Tags()); err != nil {
				return err
			}
		}
		frame, e := s.Add(p, t, f, t2)
		if e != nil {
			return e
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Adding project %s%s, started %s and stopped %s. (id: %s)\n", styleText("project", frame.Project), formatTags(frame.Tags), styleText("time", humanize(f, s.Now())), styleText("time", humanize(t2, s.Now())), styleText("id", shortID(frame.ID)))
		return nil
	}}
	cmd.Flags().StringVarP(&from, "from", "f", "", "date and time activity started")
	cmd.Flags().StringVarP(&to, "to", "t", "", "date and time activity ended")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	cmd.Flags().BoolVarP(&confirmProject, "confirm-new-project", "c", false, "confirm addition of new project")
	cmd.Flags().BoolVarP(&confirmTag, "confirm-new-tag", "b", false, "confirm creation of new tag")
	cmd.ValidArgsFunction = a.completeProjectOrTag
	return cmd
}

func confirmNewProject(reader *bufio.Reader, cmd *cobra.Command, project string, known []string) error {
	for _, value := range known {
		if value == project {
			return nil
		}
	}
	return confirmPrompt(reader, cmd, fmt.Sprintf("Project '%s' does not exist yet. Create it?", project))
}
func confirmNewTags(reader *bufio.Reader, cmd *cobra.Command, tags, known []string) error {
	for _, tag := range tags {
		exists := false
		for _, value := range known {
			if value == tag {
				exists = true
				break
			}
		}
		if !exists {
			if err := confirmPrompt(reader, cmd, fmt.Sprintf("Tag '%s' does not exist yet. Create it?", tag)); err != nil {
				return err
			}
		}
	}
	return nil
}
func confirmPrompt(reader *bufio.Reader, cmd *cobra.Command, message string) error {
	fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N]: ", message)
	answer, _ := reader.ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		return errors.New("Aborted!")
	}
	return nil
}

func (a *app) remove() *cobra.Command {
	var force bool
	cmd := &cobra.Command{Use: "remove ID", Short: "Remove a frame.", Args: cobra.ExactArgs(1), FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true}, RunE: func(cmd *cobra.Command, args []string) error {
		s, e := a.openData()
		if e != nil {
			return e
		}
		f, _, e := s.Lookup(args[0])
		if e != nil {
			return e
		}
		if !force {
			loc := watsonDisplayLocation(s.Now())
			fmt.Fprintf(cmd.OutOrStdout(), "You are about to remove frame %s%s from %s to %s, continue? [y/N]: ", styleText("project", f.Project), formatTags(f.Tags), styleText("time", time.Unix(f.Start, 0).In(loc).Format("15:04")), styleText("time", time.Unix(*f.Stop, 0).In(loc).Format("15:04")))
			line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
			if strings.ToLower(strings.TrimSpace(line)) != "y" && strings.ToLower(strings.TrimSpace(line)) != "yes" {
				return errors.New("Aborted!")
			}
		}
		f, e = s.Remove(args[0])
		if e != nil {
			return e
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Frame removed.")
		return nil
	}}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "do not ask for confirmation")
	cmd.ValidArgsFunction = a.completeFrames
	return cmd
}

func (a *app) projects() *cobra.Command {
	return listCommand("projects", "Display existing projects.", "project", func(s *watson.Service) []string { return s.Projects() }, a)
}
func (a *app) tags() *cobra.Command {
	return listCommand("tags", "Display existing tags.", "tag", func(s *watson.Service) []string { return s.Tags() }, a)
}
func (a *app) frames() *cobra.Command {
	return listCommand("frames", "Display all frame IDs.", "id", func(s *watson.Service) []string {
		out := make([]string, len(s.Frames))
		for i, f := range s.Frames {
			out[i] = shortID(f.ID)
		}
		return out
	}, a)
}
func listCommand(name, short, kind string, values func(*watson.Service) []string, a *app) *cobra.Command {
	return &cobra.Command{Use: name, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, e := a.openData()
		if e != nil {
			return e
		}
		for _, v := range values(s) {
			fmt.Fprintln(cmd.OutOrStdout(), styleText(kind, v))
		}
		return nil
	}}
}

func formatTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	styled := make([]string, len(tags))
	for i, tag := range tags {
		styled[i] = styleText("tag", tag)
	}
	return " [" + strings.Join(styled, ", ") + "]"
}

var stylingEnabled bool

func styleText(kind, value string) string {
	if !stylingEnabled {
		return value
	}
	codes := map[string]string{"project": "35", "tag": "34", "time": "32", "date": "36", "id": "37", "error": "31"}
	code := codes[kind]
	if code == "" {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}
func shortID(id string) string {
	if len(id) > 7 {
		return id[:7]
	}
	return id
}
func humanize(then, now time.Time) string {
	d := now.Sub(then)
	future := d < 0
	if future {
		d = -d
	}
	seconds := int64(d / time.Second)
	var text string
	switch {
	case d < 10*time.Second:
		return "just now"
	case d < time.Minute:
		text = fmt.Sprintf("%d seconds", seconds)
	case d < 2*time.Minute:
		text = "a minute"
	case d < time.Hour:
		text = fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 2*time.Hour:
		text = "an hour"
	case d < 24*time.Hour:
		text = fmt.Sprintf("%d hours", int(d.Hours()))
	case d < 48*time.Hour:
		text = "a day"
	case d < 7*24*time.Hour:
		text = fmt.Sprintf("%d days", int(d.Hours()/24))
	case d < 14*24*time.Hour:
		text = "a week"
	case d < 20*24*time.Hour:
		text = fmt.Sprintf("%d weeks", int(d.Hours()/(24*7)))
	case d < 365*24*time.Hour:
		months := int(float64(d)/(30*24*float64(time.Hour)) + .5)
		if months < 1 {
			months = 1
		}
		if months == 1 {
			text = "a month"
		} else {
			text = fmt.Sprintf("%d months", months)
		}
	case d < 2*365*24*time.Hour:
		text = "a year"
	default:
		text = fmt.Sprintf("%d years", int(d.Hours()/(24*365)))
	}
	if future {
		return "in " + text
	}
	return text + " ago"
}
func pythonStrftime(t time.Time, format string) string {
	var output strings.Builder
	for i := 0; i < len(format); i++ {
		if format[i] != '%' || i+1 >= len(format) {
			output.WriteByte(format[i])
			continue
		}
		i++
		modifier := byte(0)
		if strings.ContainsRune("-_0^#", rune(format[i])) && i+1 < len(format) {
			modifier = format[i]
			i++
		}
		value, ok := strftimeDirective(t, format[i])
		if !ok {
			output.WriteByte('%')
			if modifier != 0 {
				output.WriteByte(modifier)
			}
			output.WriteByte(format[i])
			continue
		}
		switch modifier {
		case '-':
			trimmed := strings.TrimLeft(value, " 0")
			if trimmed == "" && value != "" {
				trimmed = "0"
			}
			value = trimmed
		case '_':
			for len(value) > 1 && value[0] == '0' {
				value = " " + value[1:]
			}
		case '0':
			value = strings.ReplaceAll(value, " ", "0")
		case '^':
			value = strings.ToUpper(value)
		case '#':
			var swapped strings.Builder
			for _, r := range value {
				if unicode.IsUpper(r) {
					swapped.WriteRune(unicode.ToLower(r))
				} else {
					swapped.WriteRune(unicode.ToUpper(r))
				}
			}
			value = swapped.String()
		}
		output.WriteString(value)
	}
	return output.String()
}

func strftimeDirective(t time.Time, directive byte) (string, bool) {
	year, month, day := t.Date()
	hour := t.Hour()
	hour12 := hour % 12
	if hour12 == 0 {
		hour12 = 12
	}
	isoYear, isoWeek := t.ISOWeek()
	weekday := int(t.Weekday())
	mondayWeekday := (weekday + 6) % 7
	weekSunday := (t.YearDay() - 1 + 7 - weekday) / 7
	weekMonday := (t.YearDay() - 1 + 7 - mondayWeekday) / 7
	name, offset := t.Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	zoneOffset := fmt.Sprintf("%s%02d%02d", sign, offset/3600, offset%3600/60)
	amPM := "AM"
	if hour >= 12 {
		amPM = "PM"
	}
	switch directive {
	case '%':
		return "%", true
	case 'a':
		return t.Format("Mon"), true
	case 'A':
		return t.Format("Monday"), true
	case 'b', 'h':
		return t.Format("Jan"), true
	case 'B':
		return t.Format("January"), true
	case 'c':
		return fmt.Sprintf("%s %s %2d %02d:%02d:%02d %04d", t.Format("Mon"), t.Format("Jan"), day, hour, t.Minute(), t.Second(), year), true
	case 'C':
		return fmt.Sprintf("%02d", year/100), true
	case 'd':
		return fmt.Sprintf("%02d", day), true
	case 'D':
		return fmt.Sprintf("%02d/%02d/%02d", month, day, year%100), true
	case 'e':
		return fmt.Sprintf("%2d", day), true
	case 'f':
		return fmt.Sprintf("%06d", t.Nanosecond()/1000), true
	case 'F':
		return fmt.Sprintf("%04d-%02d-%02d", year, month, day), true
	case 'g':
		return fmt.Sprintf("%02d", isoYear%100), true
	case 'G':
		return fmt.Sprintf("%04d", isoYear), true
	case 'H':
		return fmt.Sprintf("%02d", hour), true
	case 'I':
		return fmt.Sprintf("%02d", hour12), true
	case 'j':
		return fmt.Sprintf("%03d", t.YearDay()), true
	case 'k':
		return fmt.Sprintf("%2d", hour), true
	case 'l':
		return fmt.Sprintf("%2d", hour12), true
	case 'm':
		return fmt.Sprintf("%02d", month), true
	case 'M':
		return fmt.Sprintf("%02d", t.Minute()), true
	case 'n':
		return "\n", true
	case 'p':
		return amPM, true
	case 'P':
		return strings.ToLower(amPM), true
	case 'r':
		return fmt.Sprintf("%02d:%02d:%02d %s", hour12, t.Minute(), t.Second(), amPM), true
	case 'R':
		return fmt.Sprintf("%02d:%02d", hour, t.Minute()), true
	case 's':
		return strconv.FormatInt(t.Unix(), 10), true
	case 'S':
		return fmt.Sprintf("%02d", t.Second()), true
	case 't':
		return "\t", true
	case 'T', 'X':
		return fmt.Sprintf("%02d:%02d:%02d", hour, t.Minute(), t.Second()), true
	case 'u':
		if weekday == 0 {
			weekday = 7
		}
		return strconv.Itoa(weekday), true
	case 'U':
		return fmt.Sprintf("%02d", weekSunday), true
	case 'V':
		return fmt.Sprintf("%02d", isoWeek), true
	case 'w':
		return strconv.Itoa(weekday), true
	case 'W':
		return fmt.Sprintf("%02d", weekMonday), true
	case 'x':
		return fmt.Sprintf("%02d/%02d/%02d", month, day, year%100), true
	case 'y':
		return fmt.Sprintf("%02d", year%100), true
	case 'Y':
		return fmt.Sprintf("%04d", year), true
	case 'z':
		return zoneOffset, true
	case 'Z':
		return name, true
	default:
		return "", false
	}
}
