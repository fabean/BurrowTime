// Package integrations owns export planning and receipts. Connector executables
// only translate protocol messages into provider API calls.
package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

const ProtocolVersion = 1

type Rounding struct {
	Mode      string `json:"mode"`
	Increment string `json:"increment"`
}

func (r Rounding) Seconds(seconds int64) (int64, error) {
	if seconds <= 0 {
		return 0, fmt.Errorf("duration must be positive")
	}
	if r.Mode == "" || r.Mode == "off" {
		return seconds, nil
	}
	if r.Mode != "up" && r.Mode != "nearest" {
		return 0, fmt.Errorf("rounding mode must be off, up, or nearest")
	}
	d, err := time.ParseDuration(r.Increment)
	if err != nil || d < time.Second || d > 24*time.Hour || d%time.Second != 0 {
		return 0, fmt.Errorf("rounding increment must be whole seconds between 1s and 24h")
	}
	n := int64(d / time.Second)
	q, remainder := seconds/n, seconds%n
	if (r.Mode == "up" && remainder > 0) || (r.Mode == "nearest" && remainder >= (n+1)/2) {
		q++
	}
	if q > (1<<63-1)/n {
		return 0, fmt.Errorf("rounded duration overflows")
	}
	return q * n, nil
}

type Connection struct {
	Plugin      string   `json:"plugin"`
	BaseURL     string   `json:"base_url,omitempty"`
	WorkspaceID string   `json:"workspace_id"`
	UserID      string   `json:"user_id"`
	APIKeyEnv   string   `json:"api_key_env"`
	Rounding    Rounding `json:"rounding"`
}

type Mapping struct {
	Connection string    `json:"connection"`
	ProjectID  string    `json:"project_id"`
	Rounding   *Rounding `json:"rounding,omitempty"`
	Billable   bool      `json:"billable"`
}

type Config struct {
	Version     int                           `json:"version"`
	Connections map[string]Connection         `json:"connections"`
	Projects    map[string]Mapping            `json:"projects"`
	Routes      map[string]map[string]Mapping `json:"routes,omitempty"`
}

func (c Config) Mapping(connection, project string) (Mapping, bool) {
	if routes := c.Routes[connection]; routes != nil {
		m, ok := routes[project]
		return m, ok
	}
	m, ok := c.Projects[project]
	return m, ok && m.Connection == connection
}

// Entry is an export projection, never a replacement for a local frame.
type Entry struct {
	Start       string `json:"start"`
	End         string `json:"end"`
	ProjectID   string `json:"projectId"`
	Description string `json:"description"`
	Billable    bool   `json:"billable"`
}

type Project struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ClientID   string `json:"client_id,omitempty"`
	ClientName string `json:"client_name,omitempty"`
}

func (p Project) ClientLabel() string {
	if p.ClientName != "" {
		return p.ClientName
	}
	if p.ClientID != "" {
		return "Unknown client [" + p.ClientID + "]"
	}
	return "No client"
}

type Request struct {
	Version    int        `json:"version"`
	Operation  string     `json:"operation"`
	Connection Connection `json:"connection"`
	Entry      Entry      `json:"entry,omitempty"`
	FrameID    string     `json:"frame_id,omitempty"`
	RemoteID   string     `json:"remote_id,omitempty"`
}

type Response struct {
	Version  int       `json:"version"`
	Error    string    `json:"error,omitempty"`
	RemoteID string    `json:"remote_id,omitempty"`
	UserID   string    `json:"user_id,omitempty"`
	Entry    *Entry    `json:"entry,omitempty"`
	Projects []Project `json:"projects,omitempty"`
}

type Caller func(context.Context, Request) (Response, error)

// Call executes an explicitly selected connector, never a shell command. Keys
// are read by the connector from the named environment variable, not serialized.
func Call(ctx context.Context, request Request) (Response, error) {
	if request.Connection.Plugin != "clockify" && request.Connection.Plugin != "timetable" {
		return Response{}, fmt.Errorf("unsupported connector %q", request.Connection.Plugin)
	}
	path, err := exec.LookPath("burrowtime-" + request.Connection.Plugin)
	if err != nil {
		return Response{}, fmt.Errorf("install the optional connector: go install github.com/fabean/BurrowTime/cmd/burrowtime-%s@latest", request.Connection.Plugin)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	request.Version = ProtocolVersion
	data, err := json.Marshal(request)
	if err != nil {
		return Response{}, err
	}
	cmd := exec.CommandContext(ctx, path)
	cmd.Stdin = bytes.NewReader(data)
	var output limitedBuffer
	cmd.Stdout = &output
	// Never forward arbitrary connector stderr, which may contain credentials.
	if err := cmd.Run(); err != nil {
		return Response{}, fmt.Errorf("connector failed: %w", err)
	}
	var response Response
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		return response, fmt.Errorf("invalid connector response")
	}
	if response.Version != ProtocolVersion {
		return response, fmt.Errorf("unsupported connector protocol %d", response.Version)
	}
	if response.Error != "" {
		return response, fmt.Errorf("connector: %s", response.Error)
	}
	return response, nil
}

type limitedBuffer struct{ bytes.Buffer }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 8<<20 {
		return 0, fmt.Errorf("connector response exceeds 8 MiB")
	}
	return b.Buffer.Write(p)
}
