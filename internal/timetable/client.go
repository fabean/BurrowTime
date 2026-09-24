// Package timetable translates the connector protocol into Timetable API calls.
package timetable

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/fabean/BurrowTime/internal/integrations"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
	Token   string
}

func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"), Token: token,
		HTTP: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

func (c *Client) request(ctx context.Context, method, path string, body any, result any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return fmt.Errorf("invalid Timetable request")
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("Timetable transport failure; request outcome may be uncertain")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Timetable HTTP %d; check token, project access, and entry data", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil || len(data) > 8<<20 {
		return fmt.Errorf("cannot read Timetable response")
	}
	if err := json.Unmarshal(data, result); err != nil {
		return fmt.Errorf("invalid Timetable JSON response")
	}
	return nil
}

func (c *Client) Handle(ctx context.Context, r integrations.Request) (integrations.Response, error) {
	response := integrations.Response{Version: integrations.ProtocolVersion}
	if r.Version != integrations.ProtocolVersion {
		return response, fmt.Errorf("unsupported protocol version")
	}
	if err := integrations.ValidateConnection(r.Connection); err != nil {
		return response, err
	}
	if err := ValidURL(r.Connection.BaseURL); err != nil {
		return response, err
	}
	if r.Connection.Plugin != "timetable" || strings.TrimRight(r.Connection.BaseURL, "/") != c.BaseURL {
		return response, fmt.Errorf("invalid Timetable connection")
	}
	if c.Token == "" {
		return response, fmt.Errorf("token environment variable is empty")
	}
	switch r.Operation {
	case "check":
		var me struct {
			ID     string `json:"id"`
			UserID string `json:"user_id"`
			User   struct {
				ID string `json:"id"`
			} `json:"user"`
		}
		if err := c.request(ctx, http.MethodGet, "/api/me", nil, &me); err != nil {
			return response, err
		}
		response.UserID = me.ID
		if me.UserID != "" {
			response.UserID = me.UserID
		}
		if me.User.ID != "" {
			response.UserID = me.User.ID
		}
		if response.UserID == "" || response.UserID != r.Connection.UserID {
			return response, fmt.Errorf("token does not match configured Timetable user")
		}
	case "projects":
		var raw json.RawMessage
		if err := c.request(ctx, http.MethodGet, "/api/projects", nil, &raw); err != nil {
			return response, err
		}
		var projects []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Archived   bool   `json:"archived"`
			ClientID   string `json:"client_id"`
			ClientName string `json:"client_name"`
			Client     struct {
				Name string `json:"name"`
			} `json:"client"`
		}
		if len(raw) > 0 && raw[0] == '{' {
			var wrapped struct {
				Projects json.RawMessage `json:"projects"`
			}
			if err := json.Unmarshal(raw, &wrapped); err != nil {
				return response, fmt.Errorf("invalid Timetable projects")
			}
			raw = wrapped.Projects
		}
		if err := json.Unmarshal(raw, &projects); err != nil {
			return response, fmt.Errorf("invalid Timetable projects")
		}
		for _, p := range projects {
			if p.Archived || p.ID == "" {
				continue
			}
			name := p.ClientName
			if name == "" {
				name = p.Client.Name
			}
			response.Projects = append(response.Projects, integrations.Project{ID: p.ID, Name: p.Name, ClientID: p.ClientID, ClientName: name})
		}
	case "create":
		start, e1 := time.Parse(time.RFC3339, r.Entry.Start)
		end, e2 := time.Parse(time.RFC3339, r.Entry.End)
		if e1 != nil || e2 != nil || !end.After(start) || r.Entry.ProjectID == "" || r.FrameID == "" || len(r.FrameID) > 128 {
			return response, fmt.Errorf("invalid completed entry or frame ID")
		}
		body := struct {
			ProjectID   string `json:"project_id"`
			StartedAt   string `json:"started_at"`
			EndedAt     string `json:"ended_at"`
			Description string `json:"description"`
			Source      string `json:"source"`
			ExternalID  string `json:"external_id"`
		}{r.Entry.ProjectID, start.Format("2006-01-02T15:04:05-07:00"), end.Format("2006-01-02T15:04:05-07:00"), r.Entry.Description, "burrowtime", r.FrameID}
		var created struct {
			Entry struct {
				ID string `json:"id"`
			} `json:"entry"`
		}
		if err := c.request(ctx, http.MethodPost, "/api/entries", body, &created); err != nil {
			return response, err
		}
		if created.Entry.ID == "" {
			return response, fmt.Errorf("Timetable omitted the entry ID")
		}
		response.RemoteID = created.Entry.ID
	default:
		return response, fmt.Errorf("unsupported connector operation")
	}
	return response, nil
}

func Serve(in io.Reader, out io.Writer) error {
	var request integrations.Request
	data, err := io.ReadAll(io.LimitReader(in, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return fmt.Errorf("request too large or unreadable")
	}
	if err := json.Unmarshal(data, &request); err != nil {
		return fmt.Errorf("invalid request")
	}
	key := os.Getenv(request.Connection.APIKeyEnv)
	response, err := New(request.Connection.BaseURL, key).Handle(context.Background(), request)
	if err != nil {
		response = integrations.Response{Version: integrations.ProtocolVersion, Error: err.Error()}
		if key != "" {
			response.Error = strings.ReplaceAll(response.Error, key, "[redacted]")
		}
	}
	return json.NewEncoder(out).Encode(response)
}

func ValidURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return fmt.Errorf("Timetable URL must be an origin, such as https://timetable.bluedroplabs.com")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1")) {
		return fmt.Errorf("Timetable URL must use HTTPS (HTTP is allowed for localhost)")
	}
	return nil
}
