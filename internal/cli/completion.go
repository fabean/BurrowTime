package cli

import (
	"strings"

	"github.com/spf13/cobra"
)

func (a *app) completeProjects(_ *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
	s, err := a.openData()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	return matching(s.Projects(), prefix), cobra.ShellCompDirectiveNoFileComp
}
func (a *app) completeTags(_ *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
	s, err := a.openData()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	return matching(s.Tags(), prefix), cobra.ShellCompDirectiveNoFileComp
}
func (a *app) completeFrames(_ *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
	s, err := a.openData()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	values := make([]string, 0, len(s.Frames))
	for _, frame := range s.Frames {
		values = append(values, frame.ID)
	}
	return matching(values, prefix), cobra.ShellCompDirectiveNoFileComp
}

func (a *app) completeTimers(_ *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
	s, err := a.openData()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	values := make([]string, 0, len(s.RunningTimers()))
	for _, timer := range s.RunningTimers() {
		values = append(values, timer.ID)
	}
	return matching(values, prefix), cobra.ShellCompDirectiveNoFileComp
}
func (a *app) completeProjectOrTag(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
	tags := strings.HasPrefix(prefix, "+")
	if !tags {
		for _, arg := range args {
			if strings.HasPrefix(arg, "+") {
				tags = true
				break
			}
		}
	}
	if !tags {
		return a.completeProjects(cmd, args, prefix)
	}
	rawPrefix := strings.TrimPrefix(prefix, "+")
	values, _ := a.completeTags(cmd, args, rawPrefix)
	for i := range values {
		values[i] = "+" + values[i]
	}
	return values, cobra.ShellCompDirectiveNoFileComp
}
func (a *app) completeRename(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return matching([]string{"project", "tag"}, prefix), cobra.ShellCompDirectiveNoFileComp
	}
	if len(args) == 1 {
		if args[0] == "project" {
			return a.completeProjects(cmd, args, prefix)
		}
		if args[0] == "tag" {
			return a.completeTags(cmd, args, prefix)
		}
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}
func (a *app) bindQueryCompletions(cmd *cobra.Command) {
	_ = cmd.RegisterFlagCompletionFunc("project", a.completeProjects)
	_ = cmd.RegisterFlagCompletionFunc("tag", a.completeTags)
	if cmd.Flags().Lookup("ignore-project") != nil {
		_ = cmd.RegisterFlagCompletionFunc("ignore-project", a.completeProjects)
	}
	if cmd.Flags().Lookup("ignore-tag") != nil {
		_ = cmd.RegisterFlagCompletionFunc("ignore-tag", a.completeTags)
	}
}
func matching(values []string, prefix string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		if strings.HasPrefix(value, prefix) && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}
