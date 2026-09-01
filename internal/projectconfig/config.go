package projectconfig

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Agent struct {
	Project        string
	Task           string
	Repository     string
	Lease          time.Duration
	TaskFromBranch bool
}

type Config struct {
	Path  string
	Agent Agent
}

func Load(startDir string) (Config, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return Config{}, err
	}
	for {
		path := filepath.Join(dir, ".burrowtime.toml")
		data, readErr := os.ReadFile(path)
		if readErr == nil {
			config, parseErr := parse(data)
			config.Path = path
			return config, parseErr
		}
		if !errors.Is(readErr, os.ErrNotExist) {
			return Config{}, readErr
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Config{}, nil
		}
		dir = parent
	}
}

func parse(data []byte) (Config, error) {
	var config Config
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(stripComment(scanner.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if section != "agent" {
			continue
		}
		key, raw, ok := strings.Cut(line, "=")
		if !ok {
			return Config{}, fmt.Errorf("invalid .burrowtime.toml line %d", lineNumber)
		}
		key, raw = strings.TrimSpace(key), strings.TrimSpace(raw)
		switch key {
		case "project", "task", "repository", "lease":
			value, err := strconv.Unquote(raw)
			if err != nil {
				return Config{}, fmt.Errorf("invalid %s on .burrowtime.toml line %d: %w", key, lineNumber, err)
			}
			switch key {
			case "project":
				config.Agent.Project = value
			case "task":
				config.Agent.Task = strings.TrimPrefix(value, "+")
			case "repository":
				config.Agent.Repository = value
			case "lease":
				config.Agent.Lease, err = time.ParseDuration(value)
				if err != nil {
					return Config{}, fmt.Errorf("invalid agent lease on .burrowtime.toml line %d: %w", lineNumber, err)
				}
			}
		case "task_from_branch":
			value, err := strconv.ParseBool(raw)
			if err != nil {
				return Config{}, fmt.Errorf("invalid task_from_branch on .burrowtime.toml line %d: %w", lineNumber, err)
			}
			config.Agent.TaskFromBranch = value
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func stripComment(line string) string {
	quoted := false
	escaped := false
	for i, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && quoted {
			escaped = true
			continue
		}
		if r == '"' {
			quoted = !quoted
			continue
		}
		if r == '#' && !quoted {
			return line[:i]
		}
	}
	return line
}

var ticketPattern = regexp.MustCompile(`(?i)[a-z][a-z0-9]*-[0-9]+`)

func GitContext(dir string) (repository, branch, task string) {
	root, err := gitOutput(dir, "rev-parse", "--show-toplevel")
	if err == nil {
		repository = filepath.Base(root)
	}
	branch, _ = gitOutput(dir, "branch", "--show-current")
	if match := ticketPattern.FindString(branch); match != "" {
		task = strings.ToUpper(match)
	}
	return repository, branch, task
}

func gitOutput(dir string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", dir}, args...)
	output, err := exec.Command("git", commandArgs...).Output()
	return strings.TrimSpace(string(output)), err
}
