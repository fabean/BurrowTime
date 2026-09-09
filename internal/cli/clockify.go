package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/fabean/BurrowTime/internal/integrations"
	"github.com/fabean/BurrowTime/internal/store"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func (a *app) clockify() *cobra.Command {
	var name string
	root := &cobra.Command{Use: "clockify", Short: "Export completed time using the optional Clockify connector"}
	root.PersistentFlags().StringVar(&name, "connection", "clockify", "Named Clockify connection")
	withConfig := func(fn func(*cobra.Command, string, *integrations.Config) error) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, _ []string) error {
			dir, err := a.resolveDir()
			if err != nil {
				return err
			}
			unlock, err := integrations.Lock(dir)
			if err != nil {
				return err
			}
			defer unlock()
			config, err := integrations.LoadConfig(dir)
			if err != nil {
				return err
			}
			return fn(cmd, dir, &config)
		}
	}
	var workspace, user, keyEnv, mode, increment string
	configure := &cobra.Command{Use: "configure", Short: "Save a connection without storing the API key", Args: cobra.NoArgs}
	configure.Flags().StringVar(&workspace, "workspace", "", "Clockify workspace ID (or CLOCKIFY_WORKSPACE_ID)")
	configure.Flags().StringVar(&user, "user", "", "Clockify user ID (or CLOCKIFY_USER_ID)")
	configure.Flags().StringVar(&keyEnv, "api-key-env", "CLOCKIFY_API_KEY", "Environment variable containing the key")
	configure.Flags().StringVar(&mode, "rounding", "off", "Per-entry rounding: off, up, nearest")
	configure.Flags().StringVar(&increment, "increment", "15m", "Rounding increment")
	configure.RunE = withConfig(func(cmd *cobra.Command, dir string, config *integrations.Config) error {
		if name == "" {
			return fmt.Errorf("connection name is required")
		}
		c, exists := config.Connections[name]
		if !exists {
			c = integrations.Connection{Plugin: "clockify", WorkspaceID: os.Getenv("CLOCKIFY_WORKSPACE_ID"), UserID: os.Getenv("CLOCKIFY_USER_ID"), APIKeyEnv: keyEnv, Rounding: integrations.Rounding{Mode: mode, Increment: increment}}
		}
		if cmd.Flags().Changed("workspace") {
			c.WorkspaceID = workspace
		}
		if cmd.Flags().Changed("user") {
			c.UserID = user
		}
		if cmd.Flags().Changed("api-key-env") {
			c.APIKeyEnv = keyEnv
		}
		if cmd.Flags().Changed("rounding") {
			c.Rounding.Mode = mode
		}
		if cmd.Flags().Changed("increment") {
			c.Rounding.Increment = increment
		}
		if err := integrations.ValidateConnection(c); err != nil {
			return err
		}
		config.Connections[name] = c
		if err := integrations.SaveConfig(dir, *config); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Saved connection %s. Key is read from %s; rounding %s (%s).\n", name, c.APIKeyEnv, c.Rounding.Mode, c.Rounding.Increment)
		return nil
	})
	projects := &cobra.Command{Use: "projects", Short: "List accessible Clockify project IDs", Args: cobra.NoArgs}
	projects.RunE = withConfig(func(cmd *cobra.Command, _ string, config *integrations.Config) error {
		c, ok := config.Connections[name]
		if !ok {
			return fmt.Errorf("configure connection %q first", name)
		}
		if _, err := integrations.Call(cmd.Context(), integrations.Request{Operation: "check", Connection: c}); err != nil {
			return err
		}
		r, err := integrations.Call(cmd.Context(), integrations.Request{Operation: "projects", Connection: c})
		if err != nil {
			return err
		}
		for _, p := range r.Projects {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", p.ID, p.Name, p.ClientLabel())
		}
		return nil
	})
	var mapMode, mapIncrement string
	var billable bool
	mapping := &cobra.Command{Use: "map LOCAL_PROJECT CLOCKIFY_PROJECT_ID", Short: "Route a local project to this connection", Args: cobra.ExactArgs(2)}
	mapping.Flags().StringVar(&mapMode, "rounding", "", "Override connection rounding: off, up, nearest")
	mapping.Flags().StringVar(&mapIncrement, "increment", "", "Override rounding increment")
	mapping.Flags().BoolVar(&billable, "billable", false, "Mark exported entries billable")
	mapping.RunE = func(cmd *cobra.Command, args []string) error {
		return withConfig(func(cmd *cobra.Command, dir string, config *integrations.Config) error {
			c, ok := config.Connections[name]
			if !ok {
				return fmt.Errorf("configure connection %q first", name)
			}
			if args[0] == "" || args[1] == "" {
				return fmt.Errorf("project names and IDs cannot be empty")
			}
			m := integrations.Mapping{Connection: name, ProjectID: args[1], Billable: billable}
			if cmd.Flags().Changed("rounding") || cmd.Flags().Changed("increment") {
				r := c.Rounding
				if cmd.Flags().Changed("rounding") {
					r.Mode = mapMode
				}
				if cmd.Flags().Changed("increment") {
					r.Increment = mapIncrement
				}
				if _, err := r.Seconds(1); err != nil {
					return err
				}
				m.Rounding = &r
			}
			config.Projects[args[0]] = m
			if err := integrations.SaveConfig(dir, *config); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Mapped %q -> %s/%s\n", args[0], name, args[1])
			return nil
		})(cmd, nil)
	}
	var dryRun, today, all, yes bool
	var from, to string
	sync := &cobra.Command{Use: "sync", Short: "Upload completed entries (today by default), skipping recorded uploads", Args: cobra.NoArgs}
	sync.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without uploading or saving receipts")
	sync.Flags().BoolVar(&yes, "yes", false, "Approve configured uploads without interactive mapping or review")
	sync.Flags().BoolVar(&today, "today", false, "Select entries starting today in local time (default)")
	sync.Flags().BoolVar(&all, "all", false, "Select all recorded history")
	sync.Flags().StringVar(&from, "from", "", "First start date, YYYY-MM-DD (local time)")
	sync.Flags().StringVar(&to, "to", "", "Last start date, inclusive, YYYY-MM-DD (local time)")
	sync.RunE = withConfig(func(cmd *cobra.Command, dir string, config *integrations.Config) error {
		if (all && (today || from != "" || to != "")) || (today && (from != "" || to != "")) {
			return fmt.Errorf("choose --today, --all, or --from/--to")
		}
		opts := integrations.Options{Connection: name, DryRun: dryRun}
		if !all {
			now := time.Now()
			start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
			end := start.AddDate(0, 0, 1)
			if from != "" || to != "" {
				if from == "" || to == "" {
					return fmt.Errorf("provide both --from and --to")
				}
				var err error
				start, err = time.ParseInLocation("2006-01-02", from, time.Local)
				if err != nil {
					return err
				}
				end, err = time.ParseInLocation("2006-01-02", to, time.Local)
				if err != nil {
					return err
				}
				end = end.AddDate(0, 0, 1)
				if !end.After(start) {
					return fmt.Errorf("--to must not precede --from")
				}
			}
			opts.From = start
			opts.To = end
		}
		repo, err := store.New(dir)
		if err != nil {
			return err
		}
		frames, err := repo.LoadFrames()
		if err != nil {
			return err
		}
		input, inOK := cmd.InOrStdin().(*os.File)
		output, outOK := cmd.OutOrStdout().(*os.File)
		interactive := !yes && inOK && outOK && term.IsTerminal(int(input.Fd())) && term.IsTerminal(int(output.Fd()))
		if interactive {
			c, ok := config.Connections[name]
			if !ok {
				return fmt.Errorf("configure connection %q first", name)
			}
			if _, err := integrations.Call(cmd.Context(), integrations.Request{Operation: "check", Connection: c}); err != nil {
				return err
			}
			response, err := integrations.Call(cmd.Context(), integrations.Request{Operation: "projects", Connection: c})
			if err != nil {
				return err
			}
			p := &exportPrompter{in: cmd.InOrStdin(), out: cmd.OutOrStdout(), projects: response.Projects, dryRun: dryRun}
			if err := p.mapMissing(config, frames, &opts); err != nil {
				return err
			}
			opts.Review = func(queue []integrations.Receipt) ([]integrations.Receipt, error) {
				reviewed, err := p.review(queue)
				if err != nil {
					return nil, err
				}
				if len(reviewed) > 0 && !dryRun {
					if err := integrations.SaveConfig(dir, *config); err != nil {
						return nil, err
					}
				}
				return reviewed, nil
			}
		} else if !yes && !dryRun {
			opts.Review = func([]integrations.Receipt) ([]integrations.Receipt, error) {
				return nil, fmt.Errorf("upload requires interactive confirmation; inspect --dry-run then pass --yes for unattended sync")
			}
		}
		return integrations.Sync(cmd.Context(), dir, *config, frames, opts, integrations.Call, cmd.OutOrStdout())
	})
	status := &cobra.Command{Use: "status", Short: "Show local export receipts, including pending uploads", Args: cobra.NoArgs}
	status.RunE = withConfig(func(cmd *cobra.Command, dir string, _ *integrations.Config) error {
		ledger, err := integrations.LoadLedger(dir)
		if err != nil {
			return err
		}
		rows := []integrations.Receipt{}
		for _, r := range ledger.Records {
			if r.Connection == name {
				rows = append(rows, r)
			}
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].FrameID < rows[j].FrameID })
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(rows)
	})
	resolve := &cobra.Command{Use: "resolve FRAME_ID CLOCKIFY_ENTRY_ID", Short: "Verify an existing remote entry and complete a pending receipt", Args: cobra.ExactArgs(2)}
	resolve.RunE = func(cmd *cobra.Command, args []string) error {
		return withConfig(func(cmd *cobra.Command, dir string, config *integrations.Config) error {
			c, ok := config.Connections[name]
			if !ok {
				return fmt.Errorf("connection not configured")
			}
			if err := integrations.Resolve(cmd.Context(), dir, name, args[0], args[1], c, integrations.Call); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Verified remote entry; local receipt marked synced.")
			return nil
		})(cmd, nil)
	}
	var confirmedAbsent bool
	retry := &cobra.Command{Use: "retry FRAME_ID", Short: "Allow retry only after verifying an uncertain upload did not create an entry", Args: cobra.ExactArgs(1)}
	retry.Flags().BoolVar(&confirmedAbsent, "confirmed-not-created", false, "I verified Clockify did not create this entry; incorrect confirmation can duplicate time")
	retry.RunE = func(cmd *cobra.Command, args []string) error {
		if !confirmedAbsent {
			return fmt.Errorf("compare Clockify entries with the timestamps, project, and description in clockify status; use resolve if the entry exists, or --confirmed-not-created only after verifying it was not created")
		}
		return withConfig(func(cmd *cobra.Command, dir string, config *integrations.Config) error {
			c, ok := config.Connections[name]
			if !ok {
				return fmt.Errorf("connection not configured")
			}
			if err := integrations.RetryPending(dir, name, args[0], c); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Marked eligible for retry. Run sync to review and upload.")
			return nil
		})(cmd, nil)
	}
	root.AddCommand(configure, projects, mapping, sync, status, resolve, retry)
	return root
}
