package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fabean/BurrowTime/internal/store"
)

func TestMCPTransportDispatchAndFailures(t *testing.T) {
	a := &app{name: "burrowtime", dir: t.TempDir()}
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":"ping","method":"ping"}`,
		`{"jsonrpc":"2.0","id":2,"method":"missing"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":false}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"missing"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"burrowtime_capabilities"}}`,
	}, "\n")
	var out bytes.Buffer
	if err := a.serveMCP(strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&out)
	var responses []mcpResponse
	for decoder.More() {
		var response mcpResponse
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, response)
	}
	if len(responses) != 5 || string(responses[0].ID) != `"ping"` || responses[1].Error.Code != -32601 || responses[2].Error.Code != -32602 {
		t.Fatalf("responses=%+v", responses)
	}
	result := responses[3].Result.(map[string]any)
	if result["isError"] != true {
		t.Fatalf("tool error=%+v", result)
	}
	if err := a.serveMCP(strings.NewReader(`{`), &out); err == nil || !strings.Contains(err.Error(), "decode MCP request") {
		t.Fatalf("decode error=%v", err)
	}
	if err := a.serveMCP(strings.NewReader(`{"id":1,"method":"ping"}`), failingWriter{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("encode error=%v", err)
	}
}

func TestMCPCompleteSessionLifecycle(t *testing.T) {
	a := &app{name: "burrowtime", dir: t.TempDir()}
	value, err := a.callMCPTool("start_time", json.RawMessage(`{"client":"codex","project":"project","task":"T-1","lease":"5m","repository":"repo","branch":"main"}`))
	if err != nil {
		t.Fatal(err)
	}
	session := value.(agentSessionResult).Session
	args, err := json.Marshal(map[string]string{"session": session.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		status store.AgentSessionStatus
	}{
		{name: "heartbeat_time", status: store.AgentSessionActive},
		{name: "pause_time", status: store.AgentSessionPaused},
		{name: "resume_time", status: store.AgentSessionActive},
		{name: "stop_time", status: store.AgentSessionStopped},
	} {
		value, err := a.callMCPTool(tc.name, args)
		if err != nil || value.(agentSessionResult).Session.Status != tc.status {
			t.Fatalf("%s: value=%+v err=%v", tc.name, value, err)
		}
	}
	for _, tc := range []struct {
		name  string
		args  string
		count int
	}{
		{name: "list_time_sessions", args: `{}`, count: 1},
		{name: "list_time_sessions", args: `{"active":true}`, count: 0},
		{name: "report_agent_time", args: `{"client":"codex","project":"project","status":"stopped"}`, count: 1},
		{name: "report_agent_time", args: `{"client":"claude"}`, count: 0},
		{name: "report_agent_time", args: `{"project":"other"}`, count: 0},
		{name: "report_agent_time", args: `{"status":"paused"}`, count: 0},
		{name: "recover_time_sessions", args: `{}`, count: 0},
	} {
		t.Run(tc.name+tc.args, func(t *testing.T) {
			value, err := a.callMCPTool(tc.name, json.RawMessage(tc.args))
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Sessions []json.RawMessage `json:"sessions"`
			}
			if err := json.Unmarshal(data, &result); err != nil || len(result.Sessions) != tc.count {
				t.Fatalf("value=%s err=%v", data, err)
			}
		})
	}
}

func TestMCPToolValidationAndStorageErrors(t *testing.T) {
	for _, name := range []string{"start_time", "heartbeat_time", "pause_time", "resume_time", "stop_time", "list_time_sessions", "report_agent_time"} {
		t.Run(name, func(t *testing.T) {
			a := &app{name: "burrowtime", dir: t.TempDir()}
			if _, err := a.callMCPTool(name, json.RawMessage(`[]`)); err == nil {
				t.Fatal("accepted array arguments")
			}
			if err := os.WriteFile(filepath.Join(a.dir, "frames"), []byte("bad"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := a.callMCPTool(name, json.RawMessage(`{}`)); err == nil {
				t.Fatal("accepted corrupt storage")
			}
		})
	}
	a := &app{name: "burrowtime", dir: t.TempDir()}
	for _, tc := range []struct {
		name string
		args string
	}{
		{name: "start_time", args: `{"lease":"invalid"}`},
		{name: "report_agent_time", args: `{"status":"invalid"}`},
		{name: "missing", args: `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := a.callMCPTool(tc.name, json.RawMessage(tc.args)); err == nil {
				t.Fatal("accepted invalid tool call")
			}
		})
	}
}

func TestMCPInitializeAndTools(t *testing.T) {
	a := &app{name: "burrowtime", dir: t.TempDir()}
	response, ok := a.handleMCPRequest(mcpRequest{JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "initialize"})
	if !ok || response.Error != nil {
		t.Fatalf("initialize response: %#v", response)
	}
	result, ok := response.Result.(map[string]any)
	if !ok || result["protocolVersion"] != mcpProtocolVersion {
		t.Fatalf("initialize result: %#v", response.Result)
	}

	response, ok = a.handleMCPRequest(mcpRequest{JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: "tools/list"})
	if !ok || response.Error != nil {
		t.Fatalf("tools/list response: %#v", response)
	}
	tools := response.Result.(map[string]any)["tools"].([]mcpTool)
	if len(tools) != 9 || tools[0].Name != "start_time" || tools[len(tools)-1].Name != "burrowtime_capabilities" {
		t.Fatalf("unexpected tools: %#v", tools)
	}
}

func TestMCPStartIsIdempotentAndStopUsesSession(t *testing.T) {
	a := &app{name: "burrowtime", dir: t.TempDir()}
	arguments := json.RawMessage(`{"client":"cursor","project":"sema","task":"SEMA-158","idempotency_key":"conversation-1:SEMA-158","lease":"10m"}`)

	first, err := a.callMCPTool("start_time", arguments)
	if err != nil {
		t.Fatal(err)
	}
	firstResult := first.(agentSessionResult)
	if firstResult.Session.Client != "cursor" || firstResult.Session.IdempotencyKey != "conversation-1:SEMA-158" || firstResult.Session.LeaseSeconds != 600 {
		t.Fatalf("start result: %#v", firstResult)
	}

	second, err := a.callMCPTool("start_time", arguments)
	if err != nil {
		t.Fatal(err)
	}
	secondResult := second.(agentSessionResult)
	if !secondResult.Duplicate || secondResult.Session.ID != firstResult.Session.ID {
		t.Fatalf("idempotent result: %#v", secondResult)
	}

	stopArguments, err := json.Marshal(map[string]string{"session": firstResult.Session.ID})
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := a.callMCPTool("stop_time", stopArguments)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.(agentSessionResult).Session.Status != store.AgentSessionStopped {
		t.Fatalf("stop result: %#v", stopped)
	}
	stoppedAgain, err := a.callMCPTool("stop_time", stopArguments)
	if err != nil || !stoppedAgain.(agentSessionResult).AlreadyStopped {
		t.Fatalf("idempotent stop: %#v, %v", stoppedAgain, err)
	}
}

func TestMCPNotificationsDoNotProduceResponses(t *testing.T) {
	a := &app{name: "burrowtime", dir: t.TempDir()}
	if response, ok := a.handleMCPRequest(mcpRequest{JSONRPC: "2.0", Method: "notifications/initialized"}); ok {
		t.Fatalf("notification produced response: %#v", response)
	}
}

func TestMCPOptionalArgumentsMayBeOmitted(t *testing.T) {
	a := &app{name: "burrowtime", dir: t.TempDir()}
	result, err := a.callMCPTool("list_time_sessions", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.(map[string]any)["sessions"].([]store.AgentSession)) != 0 {
		t.Fatalf("unexpected sessions: %#v", result)
	}
}
