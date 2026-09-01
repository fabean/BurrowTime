package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	burrowskills "github.com/fabean/BurrowTime/skills"
	"github.com/spf13/cobra"
)

func (a *app) skill() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install bundled agent skills.",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(a.installSkill(), a.doctorSkill())
	return cmd
}

func (a *app) installSkill() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:       "install AGENT",
		Short:     "Install the BurrowTime tracking skill for an agent.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: supportedSkillTargets(),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := strings.ToLower(strings.TrimSpace(args[0]))
			destinations, err := a.skillDestinations(target)
			if err != nil {
				return err
			}
			source, err := burrowskills.TrackTimeWithBurrowTimeFS()
			if err != nil {
				return fmt.Errorf("load bundled skill: %w", err)
			}
			for _, destination := range destinations {
				changed, err := installSkillFiles(source, destination.Path, force)
				if err != nil {
					return err
				}
				if changed {
					fmt.Fprintf(cmd.OutOrStdout(), "Installed %s for %s at %s\n", burrowskills.TrackTimeWithBurrowTime, destination.Agents, destination.Path)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "%s for %s is already installed at %s\n", burrowskills.TrackTimeWithBurrowTime, destination.Agents, destination.Path)
				}
			}
			if target == "codex" || target == "all" {
				legacy, err := a.legacyCodexSkillDestination()
				if err != nil {
					return err
				}
				if _, err := os.Lstat(legacy); err == nil {
					fmt.Fprintf(cmd.OutOrStdout(), "Warning: legacy Codex skill remains at %s; remove it to avoid duplicate discovery.\n", legacy)
				} else if !os.IsNotExist(err) {
					return fmt.Errorf("inspect legacy Codex skill %s: %w", legacy, err)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace changed bundled skill files")
	return cmd
}

type skillDestination struct {
	Agents string
	Path   string
}

func supportedSkillTargets() []string {
	return []string{"codex", "claude", "cursor", "gemini", "opencode", "all"}
}

func (a *app) userHomeDir() (string, error) {
	if a.homeDir != "" {
		return a.homeDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return home, nil
}

func (a *app) skillDestinations(agent string) ([]skillDestination, error) {
	agent = strings.ToLower(strings.TrimSpace(agent))
	supported := false
	for _, candidate := range supportedSkillTargets() {
		if agent == candidate {
			supported = true
			break
		}
	}
	if !supported {
		return nil, fmt.Errorf("unsupported agent %q; supported agents: %s", agent, strings.Join(supportedSkillTargets(), ", "))
	}
	home, err := a.userHomeDir()
	if err != nil {
		return nil, err
	}
	portable := skillDestination{
		Agents: "Codex, Cursor, Gemini, and OpenCode",
		Path:   filepath.Join(home, ".agents", "skills", burrowskills.TrackTimeWithBurrowTime),
	}
	claude := skillDestination{
		Agents: "Claude Code",
		Path:   filepath.Join(home, ".claude", "skills", burrowskills.TrackTimeWithBurrowTime),
	}
	switch agent {
	case "claude":
		return []skillDestination{claude}, nil
	case "all":
		return []skillDestination{portable, claude}, nil
	default:
		portable.Agents = agent
		return []skillDestination{portable}, nil
	}
}

func (a *app) legacyCodexSkillDestination() (string, error) {
	root := ""
	if a.homeDir == "" {
		root = os.Getenv("CODEX_HOME")
	}
	if root == "" {
		home, err := a.userHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, ".codex")
	}
	return filepath.Join(root, "skills", burrowskills.TrackTimeWithBurrowTime), nil
}

func (a *app) doctorSkill() *cobra.Command {
	return &cobra.Command{
		Use:       "doctor [AGENT]",
		Short:     "Check installed skill files and the BurrowTime agent protocol.",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: supportedSkillTargets(),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := "codex"
			if len(args) == 1 {
				target = args[0]
			}
			destinations, err := a.skillDestinations(target)
			if err != nil {
				return err
			}
			source, err := burrowskills.TrackTimeWithBurrowTimeFS()
			if err != nil {
				return fmt.Errorf("load bundled skill: %w", err)
			}
			var problems []string
			for _, destination := range destinations {
				if err := verifySkillFiles(source, destination.Path); err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "FAIL skill for %s at %s: %v\n", destination.Agents, destination.Path, err)
					problems = append(problems, err.Error())
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "OK skill for %s at %s\n", destination.Agents, destination.Path)
			}
			if target == "codex" || target == "all" {
				legacy, legacyErr := a.legacyCodexSkillDestination()
				if legacyErr != nil {
					problems = append(problems, legacyErr.Error())
				} else if _, legacyErr := os.Lstat(legacy); legacyErr == nil {
					problem := fmt.Sprintf("legacy Codex skill at %s can cause duplicate discovery", legacy)
					fmt.Fprintf(cmd.OutOrStdout(), "FAIL %s\n", problem)
					problems = append(problems, problem)
				} else if !os.IsNotExist(legacyErr) {
					problem := fmt.Sprintf("inspect legacy Codex skill %s: %v", legacy, legacyErr)
					fmt.Fprintf(cmd.OutOrStdout(), "FAIL %s\n", problem)
					problems = append(problems, problem)
				}
			}
			data, err := a.probeCapabilities()
			if err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "FAIL burrowtime on PATH: %v\n", err)
				problems = append(problems, err.Error())
			} else {
				var capabilities capabilityDocument
				if err := json.Unmarshal(data, &capabilities); err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "FAIL burrowtime capabilities: %v\n", err)
					problems = append(problems, err.Error())
				} else if capabilities.AgentProtocol < agentProtocolVersion || !capabilities.Features["agent_sessions"] {
					problem := fmt.Sprintf("installed BurrowTime %s does not support agent protocol %d", capabilities.Version, agentProtocolVersion)
					fmt.Fprintf(cmd.OutOrStdout(), "FAIL %s\n", problem)
					problems = append(problems, problem)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "OK burrowtime %s on PATH supports agent protocol %d\n", capabilities.Version, capabilities.AgentProtocol)
				}
			}
			if len(problems) > 0 {
				return fmt.Errorf("skill doctor found %d %s", len(problems), plural(len(problems), "problem", "problems"))
			}
			return nil
		},
	}
}

func (a *app) probeCapabilities() ([]byte, error) {
	if a.capabilitiesProbe != nil {
		return a.capabilitiesProbe()
	}
	binary, err := exec.LookPath("burrowtime")
	if err != nil {
		return nil, fmt.Errorf("find burrowtime: %w", err)
	}
	output, err := exec.Command(binary, "capabilities", "--json").CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return nil, fmt.Errorf("run %s capabilities --json: %s", binary, message)
		}
		return nil, fmt.Errorf("run %s capabilities --json: %w", binary, err)
	}
	return output, nil
}

type skillFile struct {
	path string
	data []byte
}

func bundledSkillFiles(source fs.FS) ([]skillFile, []string, error) {
	var files []skillFile
	var directories []string
	err := fs.WalkDir(source, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			directories = append(directories, filepath.FromSlash(path))
			return nil
		}
		data, err := fs.ReadFile(source, path)
		if err != nil {
			return err
		}
		files = append(files, skillFile{path: filepath.FromSlash(path), data: data})
		return nil
	})
	return files, directories, err
}

func installSkillFiles(source fs.FS, destination string, force bool) (bool, error) {
	files, directories, err := bundledSkillFiles(source)
	if err != nil {
		return false, fmt.Errorf("read bundled skill: %w", err)
	}
	for _, directory := range directories {
		target := filepath.Join(destination, directory)
		info, err := os.Lstat(target)
		switch {
		case os.IsNotExist(err):
			continue
		case err != nil:
			return false, fmt.Errorf("inspect %s: %w", target, err)
		case info.Mode()&os.ModeSymlink != 0:
			return false, fmt.Errorf("refusing to install through symlink %s", target)
		case !info.IsDir():
			return false, fmt.Errorf("refusing to replace non-directory %s", target)
		}
	}

	changed := false
	for _, file := range files {
		target := filepath.Join(destination, file.path)
		info, err := os.Lstat(target)
		switch {
		case os.IsNotExist(err):
			changed = true
			continue
		case err != nil:
			return false, fmt.Errorf("inspect %s: %w", target, err)
		case info.Mode()&os.ModeSymlink != 0:
			return false, fmt.Errorf("refusing to replace symlink %s", target)
		case !info.Mode().IsRegular():
			return false, fmt.Errorf("refusing to replace non-file %s", target)
		}
		existing, err := os.ReadFile(target)
		if err != nil {
			return false, fmt.Errorf("read %s: %w", target, err)
		}
		if !bytes.Equal(existing, file.data) {
			if !force {
				return false, fmt.Errorf("skill file %s has local changes; pass --force to replace bundled files", target)
			}
			changed = true
		}
	}

	if !changed {
		return false, nil
	}
	for _, file := range files {
		target := filepath.Join(destination, file.path)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return false, fmt.Errorf("create skill directory: %w", err)
		}
		if err := os.WriteFile(target, file.data, 0o644); err != nil {
			return false, fmt.Errorf("write %s: %w", target, err)
		}
	}
	return true, nil
}

func verifySkillFiles(source fs.FS, destination string) error {
	files, _, err := bundledSkillFiles(source)
	if err != nil {
		return fmt.Errorf("read bundled skill: %w", err)
	}
	for _, file := range files {
		target := filepath.Join(destination, file.path)
		info, err := os.Lstat(target)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("missing %s", target)
			}
			return fmt.Errorf("inspect %s: %w", target, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("invalid skill file %s", target)
		}
		data, err := os.ReadFile(target)
		if err != nil {
			return fmt.Errorf("read %s: %w", target, err)
		}
		if !bytes.Equal(data, file.data) {
			return fmt.Errorf("installed skill file %s differs from the bundled version", target)
		}
	}
	return nil
}
