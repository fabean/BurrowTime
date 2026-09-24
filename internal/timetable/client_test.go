package timetable

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fabean/BurrowTime/internal/integrations"
)

func TestConnectorCreatesIdempotentTimedEntry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatal("missing bearer token")
		}
		switch r.URL.Path {
		case "/api/me":
			w.Write([]byte(`{"id":"user-1"}`))
		case "/api/projects":
			w.Write([]byte(`{"projects":[{"id":"project-1","name":"Portal","client_name":"Client"}]}`))
		case "/api/entries":
			calls++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["source"] != "burrowtime" || body["external_id"] != "frame-1" || body["project_id"] != "project-1" {
				t.Fatalf("wrong import identity: %+v", body)
			}
			if body["started_at"] != "2026-09-09T09:00:00+00:00" || body["ended_at"] != "2026-09-09T10:00:00+00:00" {
				t.Fatalf("wrong timestamps: %+v", body)
			}
			if _, ok := body["billable"]; ok {
				t.Fatal("sent unsupported billable field")
			}
			w.Write([]byte(`{"entry":{"id":"remote-1"},"duplicate":false}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	c := New(server.URL, "secret")
	conn := integrations.Connection{Plugin: "timetable", BaseURL: server.URL, UserID: "user-1", APIKeyEnv: "TIMETABLE_TOKEN", Rounding: integrations.Rounding{Mode: "off"}}
	for _, operation := range []string{"check", "projects", "create"} {
		r, err := c.Handle(context.Background(), integrations.Request{Version: 1, Operation: operation, Connection: conn, FrameID: "frame-1", Entry: integrations.Entry{Start: "2026-09-09T09:00:00Z", End: "2026-09-09T10:00:00Z", ProjectID: "project-1"}})
		if err != nil {
			t.Fatal(err)
		}
		if operation == "check" && r.UserID != "user-1" {
			t.Fatalf("wrong user: %+v", r)
		}
		if operation == "projects" && (len(r.Projects) != 1 || r.Projects[0].ClientName != "Client") {
			t.Fatalf("wrong projects: %+v", r)
		}
		if operation == "create" && r.RemoteID != "remote-1" {
			t.Fatalf("wrong entry: %+v", r)
		}
	}
	if calls != 1 {
		t.Fatalf("created %d times", calls)
	}
}
