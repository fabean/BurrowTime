package projectconfig

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadNearestAgentConfig(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "one", "two")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	data := "[agent]\nproject = \"sema\"\ntask = \"+SEMA-158\"\nrepository = \"sema-app\"\nlease = \"12m\"\ntask_from_branch = true # infer when task is absent\n"
	if err := os.WriteFile(filepath.Join(root, ".burrowtime.toml"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(nested)
	if err != nil {
		t.Fatal(err)
	}
	if config.Agent.Project != "sema" || config.Agent.Task != "SEMA-158" || config.Agent.Repository != "sema-app" || config.Agent.Lease != 12*time.Minute || !config.Agent.TaskFromBranch {
		t.Fatalf("unexpected config: %#v", config)
	}
}

func TestInvalidAgentConfigReportsLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".burrowtime.toml"), []byte("[agent]\nlease = \"later\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("expected invalid lease error")
	}
}

func TestConfigParseValidationAndComments(t *testing.T) {
	tests := []struct {
		name, data string
		invalid    bool
	}{
		{"missing assignment", "[agent]\nproject", true},
		{"unquoted string", "[agent]\nproject = bare", true},
		{"invalid bool", "[agent]\ntask_from_branch = neither", true},
		{"unknown section", "[other]\nproject = ignored", false},
		{"comment and escape", "# comment\n[agent]\nproject = \"with # and \\\" quote\" # comment\nunknown = ignored", false},
		{"long line", "[agent]\n" + strings.Repeat("x", 70000), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := parse([]byte(tt.data))
			if (err != nil) != tt.invalid {
				t.Fatalf("config=%#v err=%v", c, err)
			}
		})
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".burrowtime.toml"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("directory accepted as project config")
	}
	config, err := Load(t.TempDir())
	if err != nil || config.Path != "" {
		t.Fatalf("missing config=%#v err=%v", config, err)
	}
}

func TestGitContextInfersTaskFromCurrentBranch(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", dir}, {"-C", dir, "checkout", "-q", "-b", "feature/bt-42-description"}} {
		if data, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, data, err)
		}
	}
	repository, branch, task := GitContext(dir)
	if repository != filepath.Base(dir) || branch != "feature/bt-42-description" || task != "BT-42" {
		t.Fatalf("repository=%q branch=%q task=%q", repository, branch, task)
	}
	repository, branch, task = GitContext(t.TempDir())
	if repository != "" || branch != "" || task != "" {
		t.Fatalf("outside git repository=%q branch=%q task=%q", repository, branch, task)
	}
}
