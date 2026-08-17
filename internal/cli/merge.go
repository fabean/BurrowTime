package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fabean/BurrowTime/internal/store"
	"github.com/spf13/cobra"
)

func (a *app) merge() *cobra.Command {
	var force bool
	validateArgs := func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cobra.ExactArgs(1)(cmd, args)
		}
		if _, err := os.Stat(args[0]); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("Invalid value for 'FRAMES_WITH_CONFLICT': Path '%s' does not exist.", args[0])
			}
			return err
		}
		return cobra.ExactArgs(1)(cmd, args)
	}
	cmd := &cobra.Command{Use: "merge FRAMES_WITH_CONFLICT", Short: "Merge a conflicting Watson frames file.", Args: validateArgs, RunE: func(cmd *cobra.Command, args []string) error {
		s, err := a.openData()
		if err != nil {
			return err
		}
		conflicting, merging, err := s.MergeReport(args[0])
		if err != nil {
			return err
		}
		largest := len(s.Frames)
		if len(merging) > largest {
			largest = len(merging)
		}
		if len(conflicting) > largest {
			largest = len(conflicting)
		}
		width := len(strconv.Itoa(largest))
		fmt.Fprintf(cmd.OutOrStdout(), "%-*d frames will be left unchanged\n", width, len(s.Frames)-len(conflicting))
		fmt.Fprintf(cmd.OutOrStdout(), "%-*d frames will be merged\n", width, len(merging))
		fmt.Fprintf(cmd.OutOrStdout(), "%-*d frames will need to be resolved\n", width, len(conflicting))
		if len(conflicting) == 0 && len(merging) == 0 {
			return nil
		}
		reader := bufio.NewReader(cmd.InOrStdin())
		if !force {
			fmt.Fprint(cmd.OutOrStdout(), "Do you want to continue? [y/N]: ")
			answer, _ := reader.ReadString('\n')
			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer != "y" && answer != "yes" {
				return nil
			}
		}
		replacements := map[string]store.Frame{}
		if len(conflicting) > 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "Will resolve conflicts:")
		}
		displayLoc := watsonDisplayLocation(s.Now())
		for _, right := range conflicting {
			left, _, _ := s.Lookup(right.ID)
			printConflict(cmd, left, right, displayLoc)
			for {
				fmt.Fprint(cmd.OutOrStdout(), "Select the frame you want to keep: left or right? (L/r): ")
				answer, _ := reader.ReadString('\n')
				answer = strings.TrimSpace(answer)
				if answer == "L" {
					break
				}
				if answer == "r" {
					replacements[right.ID] = right
					break
				}
				fmt.Fprintln(cmd.ErrOrStderr(), "Error: Response should be one of [L,r]")
			}
		}
		return s.ApplyMerge(replacements, merging)
	}}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "perform the merge without the initial confirmation")
	return cmd
}

func printConflict(cmd *cobra.Command, left, right store.Frame, loc *time.Location) {
	format := func(f store.Frame, highlight bool) map[string]any {
		project := f.Project
		start := time.Unix(f.Start, 0).In(loc).Format("2006-01-02 15:04:05")
		stop := ""
		if f.Stop != nil {
			stop = time.Unix(*f.Stop, 0).In(loc).Format("2006-01-02 15:04:05")
		}
		tags := append([]string(nil), f.Tags...)
		if highlight {
			if f.Project != left.Project {
				project = "**" + project + "**"
			}
			if f.Start != left.Start {
				start = "**" + start + "**"
			}
			leftStop := int64(0)
			if left.Stop != nil {
				leftStop = *left.Stop
			}
			rightStop := int64(0)
			if f.Stop != nil {
				rightStop = *f.Stop
			}
			if rightStop != leftStop {
				stop = "**" + stop + "**"
			}
			for i, tag := range tags {
				found := false
				for _, old := range left.Tags {
					if tag == old {
						found = true
						break
					}
				}
				if !found {
					tags[i] = "**" + tag + "**"
				}
			}
		}
		return map[string]any{"project": project, "start": start, "stop": stop, "tags": tags}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "frame %s:\n", styleText("id", shortID(left.ID)))
	leftJSON, _ := marshalEditorJSON(format(left, false))
	for _, line := range strings.Split(string(leftJSON), "\n") {
		fmt.Fprintln(cmd.OutOrStdout(), "<"+line)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "---")
	rightJSON, _ := marshalEditorJSON(format(right, true))
	for _, line := range strings.Split(string(rightJSON), "\n") {
		fmt.Fprintln(cmd.OutOrStdout(), ">"+line)
	}
}
