// Package clockify translates the connector protocol into Clockify API calls.
package clockify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fabean/BurrowTime/internal/integrations"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
	Key     string
}

func New(key string) *Client {
	return &Client{BaseURL: "https://api.clockify.me/api/v1", Key: key, HTTP: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *Client) request(ctx context.Context, method, path string, body any, result any) (http.Header, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("invalid Clockify request")
	}
	req.Header.Set("X-Api-Key", c.Key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Clockify transport failure; request outcome may be uncertain")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Clockify HTTP %d; check credentials, project permissions, workspace rules, and rate limits", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil || len(data) > 8<<20 {
		return nil, fmt.Errorf("cannot read Clockify response")
	}
	if err := json.Unmarshal(data, result); err != nil {
		return nil, fmt.Errorf("invalid Clockify JSON response")
	}
	return resp.Header, nil
}

func (c *Client) Handle(ctx context.Context, r integrations.Request) (integrations.Response, error) {
	response := integrations.Response{Version: integrations.ProtocolVersion}
	if r.Version != integrations.ProtocolVersion {
		return response, fmt.Errorf("unsupported protocol version")
	}
	if err := integrations.ValidateConnection(r.Connection); err != nil {
		return response, err
	}
	if c.Key == "" {
		return response, fmt.Errorf("API key environment variable is empty")
	}
	workspace := "/workspaces/" + url.PathEscape(r.Connection.WorkspaceID)
	switch r.Operation {
	case "check":
		var user struct {
			ID string `json:"id"`
		}
		_, err := c.request(ctx, "GET", "/user", nil, &user)
		if err != nil {
			return response, err
		}
		if user.ID != r.Connection.UserID {
			return response, fmt.Errorf("API key does not match configured Clockify user")
		}
		response.UserID = user.ID
	case "projects":
		for page := 1; page <= 1000; page++ {
			var projects []struct {
				ID         string `json:"id"`
				Name       string `json:"name"`
				Archived   bool   `json:"archived"`
				ClientID   string `json:"clientId"`
				ClientName string `json:"clientName"`
			}
			headers, err := c.request(ctx, "GET", workspace+"/projects?archived=false&page-size=1000&page="+strconv.Itoa(page), nil, &projects)
			if err != nil {
				return response, err
			}
			for _, p := range projects {
				if !p.Archived && p.ID != "" {
					response.Projects = append(response.Projects, integrations.Project{ID: p.ID, Name: p.Name, ClientID: p.ClientID, ClientName: p.ClientName})
				}
			}
			last := headers.Get("Last-Page")
			if last == "true" || (last != "false" && len(projects) < 1000) {
				return response, c.resolveClients(ctx, workspace, response.Projects)
			}
		}
		return response, fmt.Errorf("too many Clockify project pages")
	case "create":
		start, e1 := time.Parse(time.RFC3339, r.Entry.Start)
		end, e2 := time.Parse(time.RFC3339, r.Entry.End)
		if e1 != nil || e2 != nil || !end.After(start) || r.Entry.ProjectID == "" {
			return response, fmt.Errorf("invalid completed entry")
		}
		var entry struct {
			ID string `json:"id"`
		}
		_, err := c.request(ctx, "POST", workspace+"/time-entries", r.Entry, &entry)
		if err != nil {
			return response, err
		}
		if entry.ID == "" {
			return response, fmt.Errorf("Clockify omitted the created entry ID")
		}
		response.RemoteID = entry.ID
	case "get":
		if r.RemoteID == "" {
			return response, fmt.Errorf("remote ID required")
		}
		var entry struct {
			ID          string `json:"id"`
			UserID      string `json:"userId"`
			ProjectID   string `json:"projectId"`
			Description string `json:"description"`
			Billable    bool   `json:"billable"`
			Interval    struct {
				Start string `json:"start"`
				End   string `json:"end"`
			} `json:"timeInterval"`
		}
		_, err := c.request(ctx, "GET", workspace+"/time-entries/"+url.PathEscape(r.RemoteID), nil, &entry)
		if err != nil {
			return response, err
		}
		start, e1 := time.Parse(time.RFC3339, entry.Interval.Start)
		end, e2 := time.Parse(time.RFC3339, entry.Interval.End)
		if e1 != nil || e2 != nil {
			return response, fmt.Errorf("remote entry is not completed")
		}
		response.RemoteID = entry.ID
		response.UserID = entry.UserID
		response.Entry = &integrations.Entry{Start: start.UTC().Format(time.RFC3339), End: end.UTC().Format(time.RFC3339), ProjectID: entry.ProjectID, Description: entry.Description, Billable: entry.Billable}
	default:
		return response, fmt.Errorf("unsupported connector operation")
	}
	return response, nil
}

// Some project responses include clientName; others expose only clientId.
// Join paginated workspace clients so the picker is useful in either case.
func (c *Client) resolveClients(ctx context.Context, workspace string, projects []integrations.Project) error {
	needed := false
	for _, p := range projects {
		if p.ClientID != "" && p.ClientName == "" {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	names := map[string]string{}
	for page := 1; page <= 1000; page++ {
		var clients []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		headers, err := c.request(ctx, "GET", workspace+"/clients?page-size=1000&page="+strconv.Itoa(page), nil, &clients)
		if err != nil {
			return fmt.Errorf("load Clockify client names: %w", err)
		}
		for _, client := range clients {
			names[client.ID] = client.Name
		}
		last := headers.Get("Last-Page")
		if last == "true" || (last != "false" && len(clients) < 1000) {
			for i := range projects {
				if projects[i].ClientName == "" {
					projects[i].ClientName = names[projects[i].ClientID]
				}
			}
			return nil
		}
	}
	return fmt.Errorf("too many Clockify client pages")
}

// Serve handles exactly one JSON request. Stdout is exclusively protocol data.
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
	response, err := New(key).Handle(context.Background(), request)
	if err != nil {
		response = integrations.Response{Version: integrations.ProtocolVersion, Error: err.Error()}
		if key != "" {
			response.Error = strings.ReplaceAll(response.Error, key, "[redacted]")
		}
	}
	return json.NewEncoder(out).Encode(response)
}
