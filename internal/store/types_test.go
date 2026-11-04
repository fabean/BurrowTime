package store

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestFrameJSONValidation(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, data, want string }{
		{"object", `{}`, "frame must be an array"},
		{"short", `[1,2,"p"]`, "4 to 6 values"},
		{"long", `[1,2,"p","id",[],3,4]`, "4 to 6 values"},
		{"start", `["bad",2,"p","id"]`, "invalid frame start"},
		{"stop", `[1,"bad","p","id"]`, "invalid frame stop"},
		{"project", `[1,2,3,"id"]`, "invalid frame project"},
		{"id", `[1,2,"p",3]`, "invalid frame id"},
		{"tags", `[1,2,"p","id",3]`, "invalid frame tags"},
		{"updated", `[1,2,"p","id",[],"bad"]`, "invalid frame updated_at"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f Frame
			err := json.Unmarshal([]byte(tt.data), &f)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want %q", err, tt.want)
			}
		})
	}
}

func TestFrameJSONReuseClearsOptionalFields(t *testing.T) {
	t.Parallel()
	stop := int64(20)
	f := Frame{Start: 10, Stop: &stop, Project: "old", ID: "old", Tags: []string{"old"}, UpdatedAt: 30, Message: "local", StartMicros: 1, StopMicros: 2}
	if err := json.Unmarshal([]byte(`[40,null,"new",null]`), &f); err != nil {
		t.Fatal(err)
	}
	if f.Stop != nil || f.ID != "" || !f.IDNull || !f.UpdatedAtMissing || f.UpdatedAt != 0 || f.Message != "" || f.StartMicros != 0 || len(f.Tags) != 0 {
		t.Fatalf("stale fields after decoding legacy frame: %#v", f)
	}
	if err := json.Unmarshal([]byte(`[50,60,"next","real",null,70]`), &f); err != nil {
		t.Fatal(err)
	}
	if f.IDNull || f.UpdatedAtMissing || f.UpdatedAt != 70 || f.Tags == nil {
		t.Fatalf("stale legacy flags: %#v", f)
	}
	data, err := json.Marshal(Frame{Start: 1, IDNull: true, Message: "private"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `[1,null,"",null,[],0]` {
		t.Fatalf("encoding=%s", data)
	}
}

func TestTimerStateOwnsItsTags(t *testing.T) {
	t.Parallel()
	timer := ActiveTimer{Project: "p", Tags: []string{"tag"}, Start: 1}
	state := timer.State()
	state.Tags[0] = "changed"
	if !timer.Running() || !state.Running() || timer.Tags[0] != "tag" || !reflect.DeepEqual(state.Tags, []string{"changed"}) {
		t.Fatalf("timer=%#v state=%#v", timer, state)
	}
	if (ActiveTimer{}).Running() || (AgentSession{Status: AgentSessionPaused}).Running() || !(AgentSession{Status: AgentSessionActive}).Running() {
		t.Fatal("running status mismatch")
	}
}
