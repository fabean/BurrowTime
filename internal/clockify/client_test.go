package clockify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fabean/BurrowTime/internal/integrations"
)

func connection() integrations.Connection {
	return integrations.Connection{Plugin: "clockify", WorkspaceID: "workspace", UserID: "user", APIKeyEnv: "CLOCKIFY_API_KEY"}
}

func TestClockifyWireCompatibility(t *testing.T) {
	var posted integrations.Entry
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("X-Api-Key") != "test-secret" {
			t.Error("missing API key")
		}
		switch r.URL.Path {
		case "/user":
			fmt.Fprint(w, `{"id":"user"}`)
		case "/workspaces/workspace/projects":
			if r.URL.Query().Get("page-size") != "1000" {
				t.Error("bad page size")
			}
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Last-Page", "false")
				fmt.Fprint(w, `[{"id":"project","name":"Client portal"}]`)
			} else {
				w.Header().Set("Last-Page", "true")
				fmt.Fprint(w, `[{"id":"second","name":"Docs"}]`)
			}
		case "/workspaces/workspace/time-entries":
			if r.Method != "POST" {
				t.Error("expected POST")
			}
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Error(err)
			}
			w.WriteHeader(201)
			fmt.Fprint(w, `{"id":"remote"}`)
		case "/workspaces/workspace/time-entries/remote":
			fmt.Fprint(w, `{"id":"remote","userId":"user","projectId":"project","description":"PORTAL-42 [burrowtime:f]","billable":true,"timeInterval":{"start":"2026-09-09T10:00:00.000Z","end":"2026-09-09T10:30:00.000Z"}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	c := New("test-secret")
	c.BaseURL = server.URL
	r := integrations.Request{Version: 1, Connection: connection(), Operation: "check"}
	if _, err := c.Handle(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	r.Operation = "projects"
	response, err := c.Handle(context.Background(), r)
	if err != nil || len(response.Projects) != 2 {
		t.Fatalf("pagination: %+v %v", response, err)
	}
	r.Operation = "create"
	r.Entry = integrations.Entry{Start: "2026-09-09T10:00:00Z", End: "2026-09-09T10:30:00Z", ProjectID: "project", Description: "PORTAL-42 [burrowtime:f]", Billable: true}
	response, err = c.Handle(context.Background(), r)
	if err != nil || response.RemoteID != "remote" || posted != r.Entry {
		t.Fatalf("create: %+v %v", response, err)
	}
	r.Operation = "get"
	r.RemoteID = "remote"
	response, err = c.Handle(context.Background(), r)
	if err != nil || response.Entry == nil || *response.Entry != r.Entry || response.UserID != "user" {
		t.Fatalf("get: %+v %v", response, err)
	}
	if requests != 5 {
		t.Fatalf("unexpected retries: %d", requests)
	}
}

func TestNoRetryOrResponseBodyLeakOnErrors(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
				fmt.Fprint(w, "test-secret sensitive provider body")
			}))
			defer server.Close()
			c := New("test-secret")
			c.BaseURL = server.URL
			_, err := c.Handle(context.Background(), integrations.Request{Version: 1, Connection: connection(), Operation: "check"})
			if err == nil || strings.Contains(err.Error(), "test-secret") {
				t.Fatal("missing error or leaked secret")
			}
			if calls != 1 {
				t.Fatal("unexpected retry")
			}
		})
	}
}

func TestRejectWrongUserAndRedirect(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if redirect {
				http.Redirect(w, r, "https://example.invalid/", 302)
			} else {
				fmt.Fprint(w, `{"id":"another-user"}`)
			}
		}))
		c := New("secret")
		c.BaseURL = server.URL
		if _, err := c.Handle(context.Background(), integrations.Request{Version: 1, Connection: connection(), Operation: "check"}); err == nil {
			t.Fatal("accepted wrong user/redirect")
		}
		server.Close()
	}
}

func TestProjectClientNamesJoinedAcrossPages(t *testing.T) {
	pages := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/workspaces/workspace/projects":
			fmt.Fprint(w, `[{"id":"a","name":"Development - US","clientId":"wine"},{"id":"b","name":"Development - US","clientId":"leica"},{"id":"c","name":"Internal"},{"id":"d","name":"Direct","clientId":"inline","clientName":"Inline client"},{"id":"e","name":"Unknown","clientId":"missing"}]`)
		case "/workspaces/workspace/clients":
			pages++
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Last-Page", "false")
				fmt.Fprint(w, `[{"id":"wine","name":"Winebow"}]`)
			} else {
				w.Header().Set("Last-Page", "true")
				fmt.Fprint(w, `[{"id":"leica","name":"Leica"}]`)
			}
		default:
			t.Errorf("unexpected URL %s", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	c := New("test-key")
	c.BaseURL = s.URL
	r, err := c.Handle(context.Background(), integrations.Request{Version: 1, Operation: "projects", Connection: connection()})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Winebow", "Leica", "No client", "Inline client", "Unknown client [missing]"}
	if len(r.Projects) != len(want) {
		t.Fatal("missing projects")
	}
	for i, p := range r.Projects {
		if p.ClientLabel() != want[i] {
			t.Fatalf("project %s: %q != %q", p.ID, p.ClientLabel(), want[i])
		}
	}
	if pages != 2 {
		t.Fatal("client pagination incomplete")
	}
}
