package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/fabean/BurrowTime/internal/projectconfig"
	"github.com/fabean/BurrowTime/internal/store"
	"github.com/fabean/BurrowTime/internal/watson"
	"github.com/spf13/cobra"
)

type agentSessionResult struct {
	Session        store.AgentSession `json:"session"`
	Duplicate      bool               `json:"duplicate,omitempty"`
	AlreadyStopped bool               `json:"already_stopped,omitempty"`
}

func (a *app) agent() *cobra.Command {
	cmd := &cobra.Command{Use: "agent", Short: "Track work owned by coding agents.", Args: cobra.NoArgs}
	cmd.AddCommand(a.agentStart(), a.agentHeartbeat(), a.agentPause(), a.agentResume(), a.agentStop(), a.agentStatus(), a.agentRecover(), a.agentReport())
	return cmd
}

func (a *app) agentStart() *cobra.Command {
	var project, task, client, owner, repository, branch, idempotencyKey, leaseText string
	var tags []string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start an agent-owned session using a standard concurrent timer.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			workingDirectory, err := os.Getwd()
			if err != nil {
				return err
			}
			context, err := projectconfig.Load(workingDirectory)
			if err != nil {
				return err
			}
			gitRepository, gitBranch, branchTask := projectconfig.GitContext(workingDirectory)
			if project == "" {
				project = context.Agent.Project
			}
			if task == "" {
				task = context.Agent.Task
				if task == "" && context.Agent.TaskFromBranch {
					task = branchTask
				}
			}
			if client == "" {
				client = os.Getenv("BURROWTIME_AGENT_CLIENT")
			}
			if repository == "" {
				repository = context.Agent.Repository
				if repository == "" {
					repository = gitRepository
				}
			}
			if branch == "" {
				branch = gitBranch
			}
			lease := context.Agent.Lease
			if leaseText != "" {
				lease, err = time.ParseDuration(leaseText)
				if err != nil {
					return fmt.Errorf("invalid agent lease: %w", err)
				}
			}
			s, err := a.open()
			if err != nil {
				return err
			}
			session, duplicate, err := s.StartAgentSession(watson.AgentStartOptions{
				Client: client, Owner: owner, Project: project, Task: task, Tags: tags,
				Repository: repository, Branch: branch, IdempotencyKey: idempotencyKey, Lease: lease,
			})
			if err != nil {
				return err
			}
			result := agentSessionResult{Session: session, Duplicate: duplicate}
			if jsonOutput {
				return writeJSON(cmd, result)
			}
			if duplicate {
				fmt.Fprintf(cmd.OutOrStdout(), "Reused agent session %s for %s +%s (status: %s)\n", shortID(session.ID), session.Project, session.Task, session.Status)
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Started agent session %s for %s +%s (timer: %s, lease: %s)\n", shortID(session.ID), session.Project, session.Task, shortID(session.TimerID), time.Duration(session.LeaseSeconds)*time.Second)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "BurrowTime project (or .burrowtime.toml)")
	cmd.Flags().StringVarP(&task, "task", "t", "", "task or ticket tag (or .burrowtime.toml/branch)")
	cmd.Flags().StringVar(&client, "client", "", "agent client name (or BURROWTIME_AGENT_CLIENT)")
	cmd.Flags().StringVar(&owner, "owner", "", "conversation, run, or agent owner identifier")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "additional tag; repeat for more")
	cmd.Flags().StringVar(&repository, "repository", "", "repository metadata")
	cmd.Flags().StringVar(&branch, "branch", "", "branch metadata")
	cmd.Flags().StringVar(&idempotencyKey, "idempotency-key", "", "stable retry key scoped to the client")
	cmd.Flags().StringVar(&leaseText, "lease", "", "session lease duration (default 30m)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output the session as JSON")
	return cmd
}

func (a *app) agentHeartbeat() *cobra.Command {
	return a.agentSessionMutation("heartbeat", "Renew an active agent session lease.", func(s *watson.Service, ref string) (agentSessionResult, error) {
		session, err := s.HeartbeatAgentSession(ref)
		return agentSessionResult{Session: session}, err
	})
}

func (a *app) agentPause() *cobra.Command {
	return a.agentSessionMutation("pause", "Pause an agent session and close its current standard timer.", func(s *watson.Service, ref string) (agentSessionResult, error) {
		session, err := s.PauseAgentSession(ref)
		return agentSessionResult{Session: session}, err
	})
}

func (a *app) agentResume() *cobra.Command {
	return a.agentSessionMutation("resume", "Resume a paused agent session with a new standard timer.", func(s *watson.Service, ref string) (agentSessionResult, error) {
		session, err := s.ResumeAgentSession(ref)
		return agentSessionResult{Session: session}, err
	})
}

func (a *app) agentStop() *cobra.Command {
	return a.agentSessionMutation("stop", "Stop an agent session without affecting other timers.", func(s *watson.Service, ref string) (agentSessionResult, error) {
		session, already, err := s.StopAgentSession(ref)
		return agentSessionResult{Session: session, AlreadyStopped: already}, err
	})
}

func (a *app) agentSessionMutation(name, description string, action func(*watson.Service, string) (agentSessionResult, error)) *cobra.Command {
	var sessionRef string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   name,
		Short: description,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := a.openData()
			if err != nil {
				return err
			}
			result, err := action(s, sessionRef)
			if err != nil {
				return err
			}
			if jsonOutput {
				return writeJSON(cmd, result)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Agent session %s is %s.\n", shortID(result.Session.ID), result.Session.Status)
			return nil
		},
	}
	cmd.Flags().StringVarP(&sessionRef, "session", "s", "", "agent session ID")
	_ = cmd.MarkFlagRequired("session")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output the session as JSON")
	return cmd
}

func (a *app) agentStatus() *cobra.Command {
	var sessionRef string
	var activeOnly, jsonOutput bool
	cmd := &cobra.Command{Use: "status", Short: "List agent-owned sessions.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := a.openData()
		if err != nil {
			return err
		}
		if _, err := s.RecoverExpiredAgentSessions(); err != nil {
			return err
		}
		sessions := append([]store.AgentSession(nil), s.AgentSessions...)
		if sessionRef != "" {
			session, _, err := s.AgentSession(sessionRef)
			if err != nil {
				return err
			}
			sessions = []store.AgentSession{session}
		}
		if activeOnly {
			filtered := sessions[:0]
			for _, session := range sessions {
				if session.Status == store.AgentSessionActive || session.Status == store.AgentSessionPaused {
					filtered = append(filtered, session)
				}
			}
			sessions = filtered
		}
		if jsonOutput {
			return writeJSON(cmd, sessions)
		}
		if len(sessions) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No agent sessions.")
			return nil
		}
		for _, session := range sessions {
			fmt.Fprintf(cmd.OutOrStdout(), "%s  %-16s  %-10s  %s +%s\n", shortID(session.ID), session.Status, session.Client, session.Project, session.Task)
		}
		return nil
	}}
	cmd.Flags().StringVarP(&sessionRef, "session", "s", "", "show one agent session")
	cmd.Flags().BoolVar(&activeOnly, "active", false, "show active and paused sessions only")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output sessions as JSON")
	return cmd
}

func (a *app) agentRecover() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{Use: "recover", Short: "Stop agent timers whose leases expired.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		s, err := a.openData()
		if err != nil {
			return err
		}
		sessions, err := s.RecoverExpiredAgentSessions()
		if err != nil {
			return err
		}
		if jsonOutput {
			return writeJSON(cmd, sessions)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Recovered %d expired agent %s.\n", len(sessions), plural(len(sessions), "session", "sessions"))
		return nil
	}}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output recovered sessions as JSON")
	return cmd
}

type agentReportRow struct {
	Session         store.AgentSession `json:"session"`
	DurationSeconds int64              `json:"duration_seconds"`
}

func (a *app) agentReport() *cobra.Command {
	var client, project, status string
	var jsonOutput bool
	cmd := &cobra.Command{Use: "report", Short: "Report time grouped by agent session.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := validateAgentStatus(status); err != nil {
			return err
		}
		s, err := a.openData()
		if err != nil {
			return err
		}
		if _, err := s.RecoverExpiredAgentSessions(); err != nil {
			return err
		}
		rows := make([]agentReportRow, 0, len(s.AgentSessions))
		for _, session := range s.AgentSessions {
			if (client != "" && session.Client != client) || (project != "" && session.Project != project) || (status != "" && string(session.Status) != status) {
				continue
			}
			rows = append(rows, agentReportRow{Session: session, DurationSeconds: agentSessionDuration(session, s)})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Session.StartedAt < rows[j].Session.StartedAt })
		if jsonOutput {
			return writeJSON(cmd, rows)
		}
		if len(rows) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No matching agent sessions.")
			return nil
		}
		for _, row := range rows {
			fmt.Fprintf(cmd.OutOrStdout(), "%s  %-10s  %-16s  %10s  %s +%s\n", shortID(row.Session.ID), row.Session.Client, row.Session.Status, formatDuration(row.DurationSeconds), row.Session.Project, row.Session.Task)
		}
		return nil
	}}
	cmd.Flags().StringVar(&client, "client", "", "filter by agent client")
	cmd.Flags().StringVar(&project, "project", "", "filter by project")
	cmd.Flags().StringVar(&status, "status", "", "filter by session status")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output report as JSON")
	return cmd
}

func agentSessionDuration(session store.AgentSession, s *watson.Service) int64 {
	frameIDs := make(map[string]bool, len(session.FrameIDs))
	for _, id := range session.FrameIDs {
		frameIDs[id] = true
	}
	var seconds int64
	for _, frame := range s.Frames {
		if frameIDs[frame.ID] && frame.Stop != nil {
			seconds += *frame.Stop - frame.Start
		}
	}
	if session.Running() {
		for _, timer := range s.RunningTimers() {
			if timer.ID == session.TimerID {
				seconds += s.Now().Unix() - timer.Start
				break
			}
		}
	}
	return seconds
}

func writeJSON(cmd *cobra.Command, value any) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func validateAgentStatus(value string) error {
	if value == "" {
		return nil
	}
	for _, candidate := range []store.AgentSessionStatus{store.AgentSessionActive, store.AgentSessionPaused, store.AgentSessionStopped, store.AgentSessionExpired, store.AgentSessionManuallyStopped, store.AgentSessionCanceled} {
		if value == string(candidate) {
			return nil
		}
	}
	return errors.New("unknown agent session status " + strings.TrimSpace(value))
}
