package cli

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
)

const agentProtocolVersion = 1

type capabilityDocument struct {
	Version         string          `json:"version"`
	AgentProtocol   int             `json:"agent_protocol"`
	Features        map[string]bool `json:"features"`
	SkillTargets    []string        `json:"skill_targets"`
	MCPProtocol     string          `json:"mcp_protocol"`
	SessionStatuses []string        `json:"session_statuses"`
}

func currentCapabilities() capabilityDocument {
	return capabilityDocument{
		Version:       Version,
		AgentProtocol: agentProtocolVersion,
		Features: map[string]bool{
			"agent_sessions":        true,
			"idempotent_start":      true,
			"leases":                true,
			"manual_stop_reconcile": true,
			"pause_resume":          true,
			"project_context":       true,
			"start_json":            true,
			"stdio_mcp":             true,
		},
		SkillTargets:    []string{"codex", "claude", "cursor", "gemini", "opencode", "all"},
		MCPProtocol:     "2025-06-18",
		SessionStatuses: []string{"active", "paused", "stopped", "expired", "manually_stopped", "canceled"},
	}
}

func (a *app) capabilities() *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{Use: "capabilities", Short: "Show machine-readable BurrowTime features.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		capabilities := currentCapabilities()
		if jsonOutput {
			return writeJSON(cmd, capabilities)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "BurrowTime %s, agent protocol %d, MCP %s\n", capabilities.Version, capabilities.AgentProtocol, capabilities.MCPProtocol)
		names := make([]string, 0, len(capabilities.Features))
		for name := range capabilities.Features {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			enabled := capabilities.Features[name]
			if enabled {
				fmt.Fprintln(cmd.OutOrStdout(), name)
			}
		}
		return nil
	}}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output capabilities as JSON")
	return cmd
}
