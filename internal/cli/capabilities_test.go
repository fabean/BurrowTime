package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCapabilitiesCommand(t *testing.T) {
	plain, err := runBurrowTimeCommand(t.TempDir(), "capabilities")
	if err != nil || !strings.Contains(plain, "agent protocol 1") || !strings.Contains(plain, "stdio_mcp") {
		t.Fatalf("plain=%q err=%v", plain, err)
	}
	out, err := runBurrowTimeCommand(t.TempDir(), "capabilities", "--json")
	var doc capabilityDocument
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || doc.AgentProtocol != agentProtocolVersion || !doc.Features["agent_sessions"] || len(doc.SessionStatuses) != 6 {
		t.Fatalf("doc=%+v err=%v", doc, err)
	}
}
