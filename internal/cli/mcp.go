package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/fabean/BurrowTime/internal/projectconfig"
	"github.com/fabean/BurrowTime/internal/store"
	"github.com/fabean/BurrowTime/internal/watson"
	"github.com/spf13/cobra"
)

const mcpProtocolVersion = "2025-06-18"

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

type mcpToolResult struct {
	Content           []mcpTextContent `json:"content"`
	StructuredContent any              `json:"structuredContent,omitempty"`
	IsError           bool             `json:"isError,omitempty"`
}

type mcpTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (a *app) mcp() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run the BurrowTime MCP server over standard input and output.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.serveMCP(cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}

func (a *app) serveMCP(input io.Reader, output io.Writer) error {
	decoder := json.NewDecoder(input)
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	for {
		var request mcpRequest
		if err := decoder.Decode(&request); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("decode MCP request: %w", err)
		}
		response, respond := a.handleMCPRequest(request)
		if !respond {
			continue
		}
		if err := encoder.Encode(response); err != nil {
			return fmt.Errorf("encode MCP response: %w", err)
		}
	}
}

func (a *app) handleMCPRequest(request mcpRequest) (mcpResponse, bool) {
	if len(request.ID) == 0 {
		return mcpResponse{}, false
	}
	response := mcpResponse{JSONRPC: "2.0", ID: request.ID}
	switch request.Method {
	case "initialize":
		response.Result = map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "burrowtime", "title": "BurrowTime", "version": Version},
			"instructions":    "Track time only when the user explicitly asks. Agent sessions use standard BurrowTime timers and exact session IDs.",
		}
	case "ping":
		response.Result = map[string]any{}
	case "tools/list":
		response.Result = map[string]any{"tools": mcpTools()}
	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			response.Error = &mcpError{Code: -32602, Message: "Invalid tools/call parameters: " + err.Error()}
			break
		}
		value, err := a.callMCPTool(params.Name, params.Arguments)
		if err != nil {
			response.Result = mcpResult(map[string]any{"error": err.Error()}, true)
			break
		}
		response.Result = mcpResult(value, false)
	default:
		response.Error = &mcpError{Code: -32601, Message: "Method not found: " + request.Method}
	}
	return response, true
}

func mcpResult(value any, isError bool) mcpToolResult {
	data, _ := json.Marshal(value)
	return mcpToolResult{
		Content:           []mcpTextContent{{Type: "text", Text: string(data)}},
		StructuredContent: value,
		IsError:           isError,
	}
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func mcpTools() []mcpTool {
	sessionInput := objectSchema(map[string]any{"session": stringProperty("Agent session ID")}, "session")
	return []mcpTool{
		{Name: "start_time", Title: "Start agent time", Description: "Start an explicitly requested agent-owned BurrowTime session. Returns an exact session ID.", InputSchema: objectSchema(map[string]any{
			"client":  stringProperty("Agent client, such as codex, claude, cursor, gemini, or opencode"),
			"project": stringProperty("BurrowTime project"), "task": stringProperty("Task or ticket tag without a leading plus"),
			"owner": stringProperty("Conversation or run identifier"), "repository": stringProperty("Repository metadata"),
			"branch": stringProperty("Branch metadata"), "idempotency_key": stringProperty("Stable retry key scoped to this client"),
			"lease": stringProperty("Lease duration, such as 30m"),
			"tags":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, "client", "project", "task")},
		{Name: "heartbeat_time", Title: "Renew agent time", Description: "Renew the lease for an active agent session.", InputSchema: sessionInput},
		{Name: "pause_time", Title: "Pause agent time", Description: "Pause a session while waiting and close its current standard timer.", InputSchema: sessionInput},
		{Name: "resume_time", Title: "Resume agent time", Description: "Resume a paused session with a new standard timer.", InputSchema: sessionInput},
		{Name: "stop_time", Title: "Stop agent time", Description: "Idempotently stop one owned session without affecting other timers.", InputSchema: sessionInput},
		{Name: "list_time_sessions", Title: "List agent sessions", Description: "List agent sessions and reconcile expired leases.", InputSchema: objectSchema(map[string]any{"active": map[string]any{"type": "boolean"}})},
		{Name: "report_agent_time", Title: "Report agent time", Description: "Report durations grouped by agent session.", InputSchema: objectSchema(map[string]any{"client": stringProperty("Filter by client"), "project": stringProperty("Filter by project"), "status": stringProperty("Filter by status")}), Annotations: map[string]any{"readOnlyHint": true}},
		{Name: "recover_time_sessions", Title: "Recover expired agent time", Description: "Stop timers for sessions whose leases expired.", InputSchema: objectSchema(map[string]any{})},
		{Name: "burrowtime_capabilities", Title: "BurrowTime capabilities", Description: "Return supported agent protocol and feature versions.", InputSchema: objectSchema(map[string]any{}), Annotations: map[string]any{"readOnlyHint": true}},
	}
}

func (a *app) callMCPTool(name string, arguments json.RawMessage) (any, error) {
	switch name {
	case "start_time":
		var input struct {
			Client         string   `json:"client"`
			Project        string   `json:"project"`
			Task           string   `json:"task"`
			Owner          string   `json:"owner"`
			Repository     string   `json:"repository"`
			Branch         string   `json:"branch"`
			IdempotencyKey string   `json:"idempotency_key"`
			Lease          string   `json:"lease"`
			Tags           []string `json:"tags"`
		}
		if err := unmarshalMCPArguments(arguments, &input); err != nil {
			return nil, err
		}
		var lease time.Duration
		var err error
		if input.Lease != "" {
			lease, err = time.ParseDuration(input.Lease)
			if err != nil {
				return nil, fmt.Errorf("invalid agent lease: %w", err)
			}
		}
		if workingDirectory, workingDirectoryErr := os.Getwd(); workingDirectoryErr == nil {
			context, contextErr := projectconfig.Load(workingDirectory)
			if contextErr != nil {
				return nil, contextErr
			}
			gitRepository, gitBranch, _ := projectconfig.GitContext(workingDirectory)
			if input.Repository == "" {
				input.Repository = context.Agent.Repository
				if input.Repository == "" {
					input.Repository = gitRepository
				}
			}
			if input.Branch == "" {
				input.Branch = gitBranch
			}
		}
		s, err := a.open()
		if err != nil {
			return nil, err
		}
		session, duplicate, err := s.StartAgentSession(watson.AgentStartOptions{
			Client: input.Client, Project: input.Project, Task: input.Task, Owner: input.Owner,
			Repository: input.Repository, Branch: input.Branch, IdempotencyKey: input.IdempotencyKey,
			Lease: lease, Tags: input.Tags,
		})
		return agentSessionResult{Session: session, Duplicate: duplicate}, err
	case "heartbeat_time", "pause_time", "resume_time", "stop_time":
		var input struct {
			Session string `json:"session"`
		}
		if err := unmarshalMCPArguments(arguments, &input); err != nil {
			return nil, err
		}
		s, err := a.openData()
		if err != nil {
			return nil, err
		}
		switch name {
		case "heartbeat_time":
			session, err := s.HeartbeatAgentSession(input.Session)
			return agentSessionResult{Session: session}, err
		case "pause_time":
			session, err := s.PauseAgentSession(input.Session)
			return agentSessionResult{Session: session}, err
		case "resume_time":
			session, err := s.ResumeAgentSession(input.Session)
			return agentSessionResult{Session: session}, err
		default:
			session, already, err := s.StopAgentSession(input.Session)
			return agentSessionResult{Session: session, AlreadyStopped: already}, err
		}
	case "list_time_sessions":
		var input struct {
			Active bool `json:"active"`
		}
		if err := unmarshalMCPArguments(arguments, &input); err != nil {
			return nil, err
		}
		s, err := a.openData()
		if err != nil {
			return nil, err
		}
		if _, err := s.RecoverExpiredAgentSessions(); err != nil {
			return nil, err
		}
		sessions := make([]store.AgentSession, 0, len(s.AgentSessions))
		for _, session := range s.AgentSessions {
			if !input.Active || session.Status == store.AgentSessionActive || session.Status == store.AgentSessionPaused {
				sessions = append(sessions, session)
			}
		}
		return map[string]any{"sessions": sessions}, nil
	case "report_agent_time":
		var input struct {
			Client  string `json:"client"`
			Project string `json:"project"`
			Status  string `json:"status"`
		}
		if err := unmarshalMCPArguments(arguments, &input); err != nil {
			return nil, err
		}
		if err := validateAgentStatus(input.Status); err != nil {
			return nil, err
		}
		s, err := a.openData()
		if err != nil {
			return nil, err
		}
		rows := []agentReportRow{}
		for _, session := range s.AgentSessions {
			if (input.Client != "" && session.Client != input.Client) || (input.Project != "" && session.Project != input.Project) || (input.Status != "" && string(session.Status) != input.Status) {
				continue
			}
			rows = append(rows, agentReportRow{Session: session, DurationSeconds: agentSessionDuration(session, s)})
		}
		return map[string]any{"sessions": rows}, nil
	case "recover_time_sessions":
		s, err := a.openData()
		if err != nil {
			return nil, err
		}
		sessions, err := s.RecoverExpiredAgentSessions()
		return map[string]any{"sessions": sessions}, err
	case "burrowtime_capabilities":
		return map[string]any{"capabilities": currentCapabilities()}, nil
	default:
		return nil, fmt.Errorf("unknown tool %s", name)
	}
}

func unmarshalMCPArguments(arguments json.RawMessage, target any) error {
	if len(bytes.TrimSpace(arguments)) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	return json.Unmarshal(arguments, target)
}
