package cli

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	burrowskills "github.com/fabean/BurrowTime/skills"
	"github.com/spf13/cobra"
)

func (a *app) skill() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install bundled agent skills.",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(a.installSkill())
	return cmd
}

func (a *app) installSkill() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:       "install AGENT",
		Short:     "Install the BurrowTime tracking skill for Codex.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"codex"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] != "codex" {
				return fmt.Errorf("unsupported agent %q; supported agents: codex", args[0])
			}
			destination, err := codexSkillDestination()
			if err != nil {
				return err
			}
			source, err := burrowskills.TrackTimeWithBurrowTimeFS()
			if err != nil {
				return fmt.Errorf("load bundled skill: %w", err)
			}
			changed, err := installSkillFiles(source, destination, force)
			if err != nil {
				return err
			}
			if changed {
				fmt.Fprintf(cmd.OutOrStdout(), "Installed %s at %s\n", burrowskills.TrackTimeWithBurrowTime, destination)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s is already installed at %s\n", burrowskills.TrackTimeWithBurrowTime, destination)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace changed bundled skill files")
	return cmd
}

func codexSkillDestination() (string, error) {
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		codexHome = filepath.Join(home, ".codex")
	}
	return filepath.Join(codexHome, "skills", burrowskills.TrackTimeWithBurrowTime), nil
}

type skillFile struct {
	path string
	data []byte
}

func installSkillFiles(source fs.FS, destination string, force bool) (bool, error) {
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
