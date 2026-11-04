package clockify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read failure") }

func TestRequestFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body                                     string
		status                                         int
		transport, unreadable, invalidURL, invalidBody bool
		want                                           string
	}{
		{name: "transport", transport: true, want: "transport failure"},
		{name: "invalid URL", invalidURL: true, want: "invalid Clockify request"},
		{name: "marshal body", invalidBody: true, want: "unsupported type"},
		{name: "unreadable", unreadable: true, want: "cannot read Clockify response"},
		{name: "oversize", body: strings.Repeat("x", (8<<20)+1), want: "cannot read Clockify response"},
		{name: "invalid JSON", body: "{", want: "invalid Clockify JSON response"},
		{name: "unauthorized", status: 401, want: "HTTP 401"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New("private-token")
			c.HTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				if tc.transport {
					return nil, errors.New("private-token")
				}
				var body io.ReadCloser = io.NopCloser(strings.NewReader(tc.body))
				if tc.unreadable {
					body = io.NopCloser(failingReader{})
				}
				status := tc.status
				if status == 0 {
					status = 200
				}
				return &http.Response{StatusCode: status, Body: body, Header: http.Header{}}, nil
			})}
			if tc.invalidURL {
				c.BaseURL = ":"
			}
			var body any
			if tc.invalidBody {
				body = make(chan int)
			}
			var result any
			_, err := c.request(context.Background(), "GET", "/", body, &result)
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "private-token") {
				t.Fatalf("got %v; want %s", err, tc.want)
			}
		})
	}
}

func TestServeRejectsMalformedAndReturnsProtocolError(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   io.Reader
		want string
	}{
		{name: "malformed", in: strings.NewReader("{"), want: "invalid request"},
		{name: "large", in: strings.NewReader(strings.Repeat("x", (1<<20)+1)), want: "request too large"},
		{name: "read failure", in: failingReader{}, want: "request too large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := Serve(tc.in, &out); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v", err)
			}
			if out.Len() != 0 {
				t.Fatal("wrote a response for unreadable request")
			}
		})
	}
	t.Run("protocol error", func(t *testing.T) {
		t.Setenv("TEST_CONNECTOR_TOKEN", "private-token")
		var out bytes.Buffer
		if err := Serve(strings.NewReader(`{"version":0,"connection":{"api_key_env":"TEST_CONNECTOR_TOKEN"}}`), &out); err != nil {
			t.Fatal(err)
		}
		var response integrations.Response
		if err := json.Unmarshal(out.Bytes(), &response); err != nil || response.Version != 1 || !strings.Contains(response.Error, "protocol") {
			t.Fatalf("got %s %v", out.String(), err)
		}
	})
	t.Run("writer failure", func(t *testing.T) {
		if err := Serve(strings.NewReader(`{}`), failingWriter{}); err == nil {
			t.Fatal("ignored write failure")
		}
	})
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failure") }

func TestHandleValidationAndProviderFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, operation, body, want string
		mutate                      func(*integrations.Request, *Client)
	}{
		{name: "protocol", want: "protocol", mutate: func(r *integrations.Request, _ *Client) { r.Version = 0 }},
		{name: "connection", want: "unsupported connector", mutate: func(r *integrations.Request, _ *Client) { r.Connection.Plugin = "unknown" }},
		{name: "other provider", want: "invalid Clockify connection", mutate: func(r *integrations.Request, _ *Client) {
			r.Connection = integrations.Connection{Plugin: "timetable", BaseURL: "https://example.test", UserID: "user", APIKeyEnv: "TOKEN"}
		}},
		{name: "empty key", want: "empty", mutate: func(_ *integrations.Request, c *Client) { c.Key = "" }},
		{name: "unsupported operation", operation: "unknown", want: "operation"},
		{name: "bad start", operation: "create", want: "invalid completed", mutate: func(r *integrations.Request, _ *Client) { r.Entry.Start = "bad" }},
		{name: "missing created ID", operation: "create", body: `{}`, want: "omitted"},
		{name: "create request failure", operation: "create", body: `bad`, want: "JSON"},
		{name: "missing remote ID", operation: "get", want: "remote ID required", mutate: func(r *integrations.Request, _ *Client) { r.RemoteID = "" }},
		{name: "incomplete remote", operation: "get", body: `{"timeInterval":{"start":"2026-01-01T00:00:00Z"}}`, want: "not completed"},
		{name: "get request failure", operation: "get", body: `bad`, want: "JSON"},
		{name: "projects request failure", operation: "projects", body: `bad`, want: "JSON"},
		{name: "clients request failure", operation: "projects", body: `[{"id":"p","clientId":"client"}]`, want: "client names"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "clients") {
					w.WriteHeader(403)
					return
				}
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			c := New("secret")
			c.BaseURL = server.URL
			r := integrations.Request{Version: 1, Connection: connection(), Operation: tc.operation, RemoteID: "remote", Entry: integrations.Entry{Start: "2026-01-01T00:00:00Z", End: "2026-01-01T00:30:00Z", ProjectID: "p"}}
			if tc.mutate != nil {
				tc.mutate(&r, c)
			}
			if _, err := c.Handle(context.Background(), r); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v want %q", err, tc.want)
			}
		})
	}
}

func TestPaginationLimits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		clients bool
	}{{name: "projects"}, {name: "clients", clients: true}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := New("key")
			c.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				body := "[]"
				if tc.clients && strings.Contains(r.URL.Path, "projects") {
					body = `[{"id":"p","clientId":"c"}]`
				}
				headers := http.Header{"Last-Page": []string{"false"}}
				if tc.clients && strings.Contains(r.URL.Path, "projects") {
					headers.Set("Last-Page", "true")
				}
				return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			_, err := c.Handle(context.Background(), integrations.Request{Version: 1, Connection: connection(), Operation: "projects"})
			if err == nil || !strings.Contains(err.Error(), "too many Clockify "+strings.TrimSuffix(tc.name, "s")+" pages") {
				t.Fatalf("got %v", err)
			}
			want := 1000
			if tc.clients {
				want++
			}
			if calls != want {
				t.Fatalf("made %d requests want %d", calls, want)
			}
		})
	}
}
