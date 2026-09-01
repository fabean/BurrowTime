package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	burrowskills "github.com/fabean/BurrowTime/skills"
)

func TestSkillInstallCodex(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	output, err := runSkillCommand("install", "codex")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(codexHome, "skills", burrowskills.TrackTimeWithBurrowTime)
	if !strings.Contains(output, "Installed "+burrowskills.TrackTimeWithBurrowTime) || !strings.Contains(output, destination) {
		t.Fatalf("unexpected install output: %q", output)
	}
	for _, relative := range []string{"SKILL.md", filepath.Join("agents", "openai.yaml")} {
		data, err := os.ReadFile(filepath.Join(destination, relative))
		if err != nil {
			t.Fatalf("read installed %s: %v", relative, err)
		}
		if len(data) == 0 {
			t.Fatalf("installed %s is empty", relative)
		}
	}

	output, err = runSkillCommand("install", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "is already installed") {
		t.Fatalf("unexpected repeat install output: %q", output)
	}
}

func TestSkillInstallProtectsLocalChanges(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	if _, err := runSkillCommand("install", "codex"); err != nil {
		t.Fatal(err)
	}

	skillFile := filepath.Join(codexHome, "skills", burrowskills.TrackTimeWithBurrowTime, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte("local changes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runSkillCommand("install", "codex"); err == nil || !strings.Contains(err.Error(), "has local changes") {
		t.Fatalf("expected local-change error, got %v", err)
	}
	if data, err := os.ReadFile(skillFile); err != nil || string(data) != "local changes\n" {
		t.Fatalf("local skill was changed: %q, %v", data, err)
	}

	if _, err := runSkillCommand("install", "codex", "--force"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(skillFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("name: track-time-with-burrowtime")) {
		t.Fatalf("force did not restore bundled skill: %q", data)
	}
}

func TestSkillInstallRejectsUnsupportedAgent(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	if _, err := runSkillCommand("install", "other-agent"); err == nil || !strings.Contains(err.Error(), "unsupported agent") {
		t.Fatalf("expected unsupported-agent error, got %v", err)
	}
}

func TestSkillInstallRejectsSymlinkedSkillDirectory(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	skillsDirectory := filepath.Join(codexHome, "skills")
	if err := os.MkdirAll(skillsDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(skillsDirectory, burrowskills.TrackTimeWithBurrowTime)
	if err := os.Symlink(t.TempDir(), destination); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := runSkillCommand("install", "codex", "--force"); err == nil || !strings.Contains(err.Error(), "refusing to install through symlink") {
		t.Fatalf("expected symlink error, got %v", err)
	}
}

func runSkillCommand(args ...string) (string, error) {
	var output bytes.Buffer
	root := (&app{name: "burrowtime"}).root()
	root.SetArgs(append([]string{"skill"}, args...))
	root.SetIn(strings.NewReader(""))
	root.SetOut(&output)
	root.SetErr(&output)
	err := root.Execute()
	return output.String(), err
}
