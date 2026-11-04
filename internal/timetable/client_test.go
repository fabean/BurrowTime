package timetable

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
		{name: "invalid URL", invalidURL: true, want: "invalid Timetable request"},
		{name: "marshal body", invalidBody: true, want: "unsupported type"},
		{name: "unreadable", unreadable: true, want: "cannot read Timetable response"},
		{name: "oversize", body: strings.Repeat("x", (8<<20)+1), want: "cannot read Timetable response"},
		{name: "invalid JSON", body: "{", want: "invalid Timetable JSON response"},
		{name: "unauthorized", status: 401, want: "HTTP 401"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New("https://example.test", "private-token")
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
			err := c.request(context.Background(), "GET", "/", body, &result)
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

func timetableRequest(baseURL string) integrations.Request {
	return integrations.Request{Version: 1, Connection: integrations.Connection{Plugin: "timetable", BaseURL: baseURL, UserID: "user", APIKeyEnv: "TOKEN"}, Operation: "check", FrameID: "frame", Entry: integrations.Entry{Start: "2026-01-01T00:00:00Z", End: "2026-01-01T01:00:00Z", ProjectID: "p"}}
}

func TestHandleValidationAndResponses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, operation, body, want string
		mutate                      func(*integrations.Request, *Client)
	}{
		{name: "protocol", want: "protocol", mutate: func(r *integrations.Request, _ *Client) { r.Version = 0 }},
		{name: "connection", want: "unsupported connector", mutate: func(r *integrations.Request, _ *Client) { r.Connection.Plugin = "unknown" }},
		{name: "URL", want: "HTTPS", mutate: func(r *integrations.Request, _ *Client) { r.Connection.BaseURL = "http://example.test" }},
		{name: "mismatched URL", want: "invalid Timetable connection", mutate: func(r *integrations.Request, _ *Client) { r.Connection.BaseURL = "https://another.test" }},
		{name: "empty token", want: "empty", mutate: func(_ *integrations.Request, c *Client) { c.Token = "" }},
		{name: "check failure", body: `bad`, want: "JSON"},
		{name: "wrong user", body: `{"id":"other"}`, want: "does not match"},
		{name: "user ID response", body: `{"user_id":"user"}`},
		{name: "nested user response", body: `{"id":"another","user_id":"other","user":{"id":"user"}}`},
		{name: "projects failure", operation: "projects", body: `bad`, want: "JSON"},
		{name: "projects invalid array", operation: "projects", body: `{"projects":5}`, want: "invalid Timetable projects"},
		{name: "projects unwrapped", operation: "projects", body: `[{"id":"p","name":"Portal","client":{"name":"Client"}},{"id":"old","archived":true},{"name":"missing"}]`},
		{name: "bad frame", operation: "create", want: "invalid completed", mutate: func(r *integrations.Request, _ *Client) { r.FrameID = "" }},
		{name: "bad start", operation: "create", want: "invalid completed", mutate: func(r *integrations.Request, _ *Client) { r.Entry.Start = "bad" }},
		{name: "create failure", operation: "create", body: `bad`, want: "JSON"},
		{name: "missing entry ID", operation: "create", body: `{}`, want: "omitted"},
		{name: "unsupported operation", operation: "get", want: "unsupported connector operation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, tc.body) }))
			defer server.Close()
			c := New(server.URL, "secret")
			r := timetableRequest(server.URL)
			if tc.operation != "" {
				r.Operation = tc.operation
			}
			if tc.mutate != nil {
				tc.mutate(&r, c)
			}
			response, err := c.Handle(context.Background(), r)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("got %v want %q", err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if r.Operation == "check" && response.UserID != "user" {
				t.Fatalf("got %+v", response)
			}
			if r.Operation == "projects" && (len(response.Projects) != 1 || response.Projects[0].ClientName != "Client") {
				t.Fatalf("got %+v", response)
			}
		})
	}
}

func TestValidURL(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, raw string
		valid     bool
	}{{name: "https", raw: "https://example.test", valid: true}, {name: "localhost", raw: "http://localhost:8080", valid: true}, {name: "loopback", raw: "http://127.0.0.1:80", valid: true}, {name: "invalid", raw: ":"}, {name: "remote http", raw: "http://example.test"}, {name: "userinfo", raw: "https://user@example.test"}, {name: "path", raw: "https://example.test/path"}, {name: "query", raw: "https://example.test?key=value"}, {name: "fragment", raw: "https://example.test#part"}} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidURL(tc.raw); (err == nil) != tc.valid {
				t.Fatalf("got %v valid %v", err, tc.valid)
			}
		})
	}
}

func TestRedirectDoesNotForwardToken(t *testing.T) {
	t.Parallel()
	redirected := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { redirected = true; fmt.Fprint(w, `{"id":"user"}`) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer origin.Close()
	c := New(origin.URL, "secret")
	if _, err := c.Handle(context.Background(), timetableRequest(origin.URL)); err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("got %v", err)
	}
	if redirected {
		t.Fatal("forwarded credentials to redirect destination")
	}
}
