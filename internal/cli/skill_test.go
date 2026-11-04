package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

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

func TestSkillHomeAndDestinations(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	a := &app{}
	if got, err := a.userHomeDir(); err != nil || got != home {
		t.Fatalf("home %q %v", got, err)
	}
	destinations, err := a.skillDestinations(" CLAUDE ")
	if err != nil || len(destinations) != 1 || destinations[0].Path != filepath.Join(home, ".claude", "skills", burrowskills.TrackTimeWithBurrowTime) {
		t.Fatalf("destinations %+v %v", destinations, err)
	}
	t.Setenv("CODEX_HOME", filepath.Join(home, "custom-codex"))
	if got, err := a.legacyCodexSkillDestination(); err != nil || got != filepath.Join(home, "custom-codex", "skills", burrowskills.TrackTimeWithBurrowTime) {
		t.Fatalf("legacy %q %v", got, err)
	}
	t.Setenv("CODEX_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	if _, err := a.userHomeDir(); err == nil {
		t.Fatal("accepted missing home directory")
	}
	if _, err := a.skillDestinations("codex"); err == nil {
		t.Fatal("accepted unavailable destination")
	}
	if _, err := a.legacyCodexSkillDestination(); err == nil {
		t.Fatal("accepted unavailable legacy destination")
	}
	if _, err := runSkillCommand(a, "doctor"); err == nil {
		t.Fatal("doctor accepted missing home")
	}
}

func TestSkillDoctorReportsBrokenInstallAndCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name, data, want string
		probeError       bool
		installed        bool
	}{
		{name: "missing install", data: `{"agent_protocol":1,"features":{"agent_sessions":true}}`, want: "FAIL skill"},
		{name: "probe failure", probeError: true, installed: true, want: "FAIL burrowtime on PATH"},
		{name: "malformed capabilities", data: "{", installed: true, want: "FAIL burrowtime capabilities"},
		{name: "missing feature", data: `{"agent_protocol":1,"features":{}}`, installed: true, want: "does not support"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &app{homeDir: t.TempDir(), capabilitiesProbe: func() ([]byte, error) {
				if tc.probeError {
					return nil, fmt.Errorf("probe failed")
				}
				return []byte(tc.data), nil
			}}
			if tc.installed {
				if _, err := runSkillCommand(a, "install", "codex"); err != nil {
					t.Fatal(err)
				}
			}
			out, err := runSkillCommand(a, "doctor")
			if err == nil || !strings.Contains(out, tc.want) || !strings.Contains(err.Error(), "found 1 problem") {
				t.Fatalf("got %q %v", out, err)
			}
		})
	}
	t.Run("unsupported", func(t *testing.T) {
		if _, err := runSkillCommand(&app{homeDir: t.TempDir()}, "doctor", "unknown"); err == nil || !strings.Contains(err.Error(), "unsupported agent") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestSkillDoctorNormalizesAgentBeforeLegacyCheck(t *testing.T) {
	a := &app{homeDir: t.TempDir(), capabilitiesProbe: func() ([]byte, error) { return json.Marshal(currentCapabilities()) }}
	if _, err := runSkillCommand(a, "install", "codex"); err != nil {
		t.Fatal(err)
	}
	legacy, err := a.legacyCodexSkillDestination()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := runSkillCommand(a, "doctor", " CODEX "); err == nil || !strings.Contains(out, "duplicate discovery") {
		t.Fatalf("got %q %v", out, err)
	}
}

func TestSkillLegacyInspectionFailure(t *testing.T) {
	a := &app{homeDir: t.TempDir(), capabilitiesProbe: func() ([]byte, error) { return json.Marshal(currentCapabilities()) }}
	if err := os.WriteFile(filepath.Join(a.homeDir, ".codex"), []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runSkillCommand(a, "install", "codex"); err == nil || !strings.Contains(err.Error(), "inspect legacy Codex") {
		t.Fatalf("got %v", err)
	}
	if out, err := runSkillCommand(a, "doctor"); err == nil || !strings.Contains(out, "inspect legacy Codex") {
		t.Fatalf("got %q %v", out, err)
	}
}

func TestProbeCapabilitiesSubprocess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable shell fixtures")
	}
	for _, tc := range []struct{ name, script, want, wantError string }{
		{name: "success", script: `test "$1" = capabilities && test "$2" = --json || exit 4; printf '{"agent_protocol":1}'`, want: `{"agent_protocol":1}`},
		{name: "malformed", script: `printf '{'`, want: "{"},
		{name: "reported error", script: `printf 'fixture failed' >&2; exit 1`, wantError: "fixture failed"},
		{name: "silent error", script: `exit 2`, wantError: "exit status 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "burrowtime"), []byte("#!/bin/sh\n"+tc.script+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			data, err := (&app{}).probeCapabilities()
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("got %q %v", data, err)
				}
			} else if err != nil || string(data) != tc.want {
				t.Fatalf("got %q %v", data, err)
			}
		})
	}
	t.Run("not installed", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if _, err := (&app{}).probeCapabilities(); err == nil || !strings.Contains(err.Error(), "find burrowtime") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestSkillFileValidation(t *testing.T) {
	source := fstest.MapFS{"SKILL.md": {Data: []byte("bundled")}, "agents/openai.yaml": {Data: []byte("agent")}}
	for _, tc := range []struct {
		name, want string
		setup      func(*testing.T, string)
	}{
		{name: "missing", want: "missing", setup: func(*testing.T, string) {}},
		{name: "directory", want: "invalid skill file", setup: func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0700); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "different", want: "differs from", setup: func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte("local"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "symlink", want: "invalid skill file", setup: func(t *testing.T, path string) {
			if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), path); err != nil {
				t.Skip(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, filepath.Join(dir, "SKILL.md"))
			if err := verifySkillFiles(source, dir); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
	t.Run("install file destination", func(t *testing.T) {
		dir := t.TempDir()
		dest := filepath.Join(dir, "skill")
		if err := os.WriteFile(dest, []byte("file"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := installSkillFiles(source, dest, true); err == nil || !strings.Contains(err.Error(), "non-directory") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("install non-file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "SKILL.md"), 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := installSkillFiles(source, dir, true); err == nil || !strings.Contains(err.Error(), "non-file") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("install symlink file", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), filepath.Join(dir, "SKILL.md")); err != nil {
			t.Skip(err)
		}
		if _, err := installSkillFiles(source, dir, true); err == nil || !strings.Contains(err.Error(), "replace symlink") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("invalid source", func(t *testing.T) {
		source := brokenSkillFS{}
		if _, _, err := bundledSkillFiles(source); err == nil {
			t.Fatal("ignored source error")
		}
		if _, err := installSkillFiles(source, t.TempDir(), false); err == nil || !strings.Contains(err.Error(), "read bundled skill") {
			t.Fatalf("got %v", err)
		}
		if err := verifySkillFiles(source, t.TempDir()); err == nil || !strings.Contains(err.Error(), "read bundled skill") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("unreadable source file", func(t *testing.T) {
		if _, _, err := bundledSkillFiles(unreadableSkillFS{FS: source}); err == nil {
			t.Fatal("ignored unreadable source file")
		}
	})
}

type brokenSkillFS struct{}

func (brokenSkillFS) Open(string) (fs.File, error) { return nil, fs.ErrPermission }

type unreadableSkillFS struct{ fs.FS }

func (unreadableSkillFS) ReadFile(string) ([]byte, error) { return nil, fs.ErrPermission }
