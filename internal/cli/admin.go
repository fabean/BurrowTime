package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	btConfig "github.com/fabean/BurrowTime/internal/config"
	"github.com/spf13/cobra"
)

func (a *app) rename() *cobra.Command {
	return &cobra.Command{Use: "rename TYPE OLD_NAME NEW_NAME", Short: "Rename a project or tag.", Args: cobra.ExactArgs(3), ValidArgs: []string{"project", "tag"}, ValidArgsFunction: a.completeRename, RunE: func(cmd *cobra.Command, args []string) error {
		if args[0] != "project" && args[0] != "tag" {
			return fmt.Errorf("You have to call rename with type \"project\" or \"tag\"; you supplied %q", args[0])
		}
		s, e := a.openData()
		if e != nil {
			return e
		}
		if e = s.Rename(args[0], args[1], args[2]); e != nil {
			return e
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Renamed %s \"%s\" to \"%s\"\n", args[0], styleText(args[0], args[1]), styleText(args[0], args[2]))
		return nil
	}}
}

func (a *app) config() *cobra.Command {
	var edit bool
	cmd := &cobra.Command{Use: "config SECTION.OPTION [VALUE]", Short: "Get and set configuration options.", Args: cobra.RangeArgs(0, 2), RunE: func(cmd *cobra.Command, args []string) error {
		if edit {
			s, e := a.open()
			if e != nil {
				return e
			}
			path := filepath.Join(s.Repo.Dir, "config")
			initial, e := os.ReadFile(path)
			if e != nil && !errors.Is(e, os.ErrNotExist) {
				return e
			}
			edited, changed, e := runEditor(initial, ".ini", cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
			if e != nil {
				return e
			}
			if !changed || len(edited) == 0 {
				return nil
			}
			if e = s.Repo.SaveConfig(edited); e != nil {
				return e
			}
			if _, e = btConfig.Load(path); e != nil {
				_ = s.Repo.SaveConfig(s.Config.Bytes())
				return fmt.Errorf("Cannot parse config file: %w", e)
			}
			return nil
		}
		if len(args) == 0 {
			return cmd.Help()
		}
		parts := strings.Split(args[0], ".")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return errors.New("The key must have the format 'section.option'")
		}
		s, e := a.open()
		if e != nil {
			return e
		}
		if len(args) == 1 {
			if !s.Config.HasSection(parts[0]) {
				return fmt.Errorf("No such section %s", parts[0])
			}
			if !s.Config.HasOption(parts[0], parts[1]) {
				return fmt.Errorf("No such option %s in %s", parts[1], parts[0])
			}
			fmt.Fprintln(cmd.OutOrStdout(), s.Config.Get(parts[0], parts[1], ""))
			return nil
		}
		if e := s.Config.Set(parts[0], parts[1], args[1]); e != nil {
			return e
		}
		return s.Repo.SaveConfig(s.Config.Bytes())
	}}
	cmd.Flags().BoolVarP(&edit, "edit", "e", false, "edit the configuration file with an editor")
	return cmd
}
