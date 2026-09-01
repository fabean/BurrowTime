package cli

import (
	"encoding/json"
	"testing"

	"github.com/fabean/BurrowTime/internal/store"
)

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
