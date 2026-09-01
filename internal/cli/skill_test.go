package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	burrowskills "github.com/fabean/BurrowTime/skills"
)

func TestSkillInstallCodex(t *testing.T) {
	home := t.TempDir()
	a := &app{name: "burrowtime", homeDir: home}

	output, err := runSkillCommand(a, "install", "codex")
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(home, ".agents", "skills", burrowskills.TrackTimeWithBurrowTime)
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

	output, err = runSkillCommand(a, "install", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "is already installed") {
		t.Fatalf("unexpected repeat install output: %q", output)
	}
}

func TestSkillInstallProtectsLocalChanges(t *testing.T) {
	home := t.TempDir()
	a := &app{name: "burrowtime", homeDir: home}
	if _, err := runSkillCommand(a, "install", "codex"); err != nil {
		t.Fatal(err)
	}

	skillFile := filepath.Join(home, ".agents", "skills", burrowskills.TrackTimeWithBurrowTime, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte("local changes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runSkillCommand(a, "install", "codex"); err == nil || !strings.Contains(err.Error(), "has local changes") {
		t.Fatalf("expected local-change error, got %v", err)
	}
	if data, err := os.ReadFile(skillFile); err != nil || string(data) != "local changes\n" {
		t.Fatalf("local skill was changed: %q, %v", data, err)
	}

	if _, err := runSkillCommand(a, "install", "codex", "--force"); err != nil {
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
	a := &app{name: "burrowtime", homeDir: t.TempDir()}
	if _, err := runSkillCommand(a, "install", "other-agent"); err == nil || !strings.Contains(err.Error(), "unsupported agent") {
		t.Fatalf("expected unsupported-agent error, got %v", err)
	}
}

func TestSkillInstallRejectsSymlinkedSkillDirectory(t *testing.T) {
	home := t.TempDir()
	a := &app{name: "burrowtime", homeDir: home}
	skillsDirectory := filepath.Join(home, ".agents", "skills")
	if err := os.MkdirAll(skillsDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(skillsDirectory, burrowskills.TrackTimeWithBurrowTime)
	if err := os.Symlink(t.TempDir(), destination); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := runSkillCommand(a, "install", "codex", "--force"); err == nil || !strings.Contains(err.Error(), "refusing to install through symlink") {
		t.Fatalf("expected symlink error, got %v", err)
	}
}

func TestSkillInstallAllUsesPortableAndClaudeLocations(t *testing.T) {
	home := t.TempDir()
	a := &app{name: "burrowtime", homeDir: home}
	output, err := runSkillCommand(a, "install", "all")
	if err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{
		filepath.Join(home, ".agents", "skills", burrowskills.TrackTimeWithBurrowTime),
		filepath.Join(home, ".claude", "skills", burrowskills.TrackTimeWithBurrowTime),
	} {
		if _, err := os.Stat(filepath.Join(destination, "SKILL.md")); err != nil {
			t.Fatalf("missing install at %s: %v", destination, err)
		}
		if !strings.Contains(output, destination) {
			t.Fatalf("output does not mention %s: %q", destination, output)
		}
	}
}

func TestSkillDoctorChecksFilesAndProtocol(t *testing.T) {
	home := t.TempDir()
	capabilities, err := json.Marshal(currentCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	a := &app{name: "burrowtime", homeDir: home, capabilitiesProbe: func() ([]byte, error) { return capabilities, nil }}
	if _, err := runSkillCommand(a, "install", "all"); err != nil {
		t.Fatal(err)
	}
	output, err := runSkillCommand(a, "doctor", "all")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(output, "OK skill") != 2 || !strings.Contains(output, "supports agent protocol 1") {
		t.Fatalf("unexpected doctor output: %q", output)
	}
}

func TestSkillDoctorRejectsOldBinary(t *testing.T) {
	home := t.TempDir()
	a := &app{name: "burrowtime", homeDir: home, capabilitiesProbe: func() ([]byte, error) {
		return []byte(`{"version":"0.1.3","agent_protocol":0,"features":{}}`), nil
	}}
	if _, err := runSkillCommand(a, "install", "codex"); err != nil {
		t.Fatal(err)
	}
	output, err := runSkillCommand(a, "doctor", "codex")
	if err == nil || !strings.Contains(err.Error(), "found 1 problem") {
		t.Fatalf("expected doctor failure, got %v", err)
	}
	if !strings.Contains(output, "does not support agent protocol 1") {
		t.Fatalf("unexpected doctor output: %q", output)
	}
}

func TestSkillInstallAndDoctorFindLegacyCodexCopy(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".codex", "skills", burrowskills.TrackTimeWithBurrowTime)
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	capabilities, err := json.Marshal(currentCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	a := &app{name: "burrowtime", homeDir: home, capabilitiesProbe: func() ([]byte, error) { return capabilities, nil }}
	output, err := runSkillCommand(a, "install", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "legacy Codex skill remains") || !strings.Contains(output, legacy) {
		t.Fatalf("missing legacy warning: %q", output)
	}
	output, err = runSkillCommand(a, "doctor", "codex")
	if err == nil || !strings.Contains(output, "duplicate discovery") {
		t.Fatalf("doctor output=%q err=%v", output, err)
	}
}

func runSkillCommand(a *app, args ...string) (string, error) {
	var output bytes.Buffer
	root := a.root()
	root.SetArgs(append([]string{"skill"}, args...))
	root.SetIn(strings.NewReader(""))
	root.SetOut(&output)
	root.SetErr(&output)
	err := root.Execute()
	return output.String(), err
}
