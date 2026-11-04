package integrations

import (
	"bytes"
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRoundingValidationAndOverflow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		rounding Rounding
		seconds  int64
		want     int64
		failure  bool
	}{
		{name: "zero", seconds: 0, failure: true},
		{name: "negative", seconds: -1, failure: true},
		{name: "default", seconds: 13, want: 13},
		{name: "odd nearest below", rounding: Rounding{Mode: "nearest", Increment: "3s"}, seconds: 4, want: 3},
		{name: "odd nearest above", rounding: Rounding{Mode: "nearest", Increment: "3s"}, seconds: 5, want: 6},
		{name: "overflow", rounding: Rounding{Mode: "up", Increment: "24h"}, seconds: math.MaxInt64, failure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.rounding.Seconds(tc.seconds)
			if (err != nil) != tc.failure || got != tc.want {
				t.Fatalf("got %d, %v; want %d failure %v", got, err, tc.want, tc.failure)
			}
		})
	}
}

func TestConfigMappingRoutesTakePrecedence(t *testing.T) {
	t.Parallel()
	c := Config{Projects: map[string]Mapping{"a": {Connection: "legacy", ProjectID: "old"}}, Routes: map[string]map[string]Mapping{"new": {"a": {Connection: "new", ProjectID: "new"}}}}
	for _, tc := range []struct {
		name, connection, project, want string
		found                           bool
	}{
		{name: "route", connection: "new", project: "a", want: "new", found: true},
		{name: "legacy", connection: "legacy", project: "a", want: "old", found: true},
		{name: "other", connection: "other", project: "a"},
		{name: "missing route", connection: "new", project: "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, ok := c.Mapping(tc.connection, tc.project)
			if ok != tc.found || (ok && m.ProjectID != tc.want) {
				t.Fatalf("got %+v, %v", m, ok)
			}
		})
	}
	for _, tc := range []struct {
		name    string
		project Project
		want    string
	}{{name: "name", project: Project{ClientName: "Client"}, want: "Client"}, {name: "id", project: Project{ClientID: "123"}, want: "Unknown client [123]"}, {name: "none", want: "No client"}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.project.ClientLabel(); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestCallConnectorProtocol(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable shell fixtures")
	}
	for _, tc := range []struct{ name, script, wantError string }{
		{name: "valid", script: `read request; printf '{"version":1,"user_id":"user"}'`},
		{name: "malformed", script: `printf '{'`, wantError: "invalid connector response"},
		{name: "version", script: `printf '{"version":2}'`, wantError: "unsupported connector protocol"},
		{name: "provider error", script: `printf '{"version":1,"error":"denied"}'`, wantError: "connector: denied"},
		{name: "failure", script: `printf 'credential-do-not-forward' >&2; exit 2`, wantError: "connector failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "burrowtime-clockify")
			if err := os.WriteFile(path, []byte("#!/bin/sh\n"+tc.script+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			response, err := Call(context.Background(), Request{Connection: Connection{Plugin: "clockify"}})
			if tc.wantError == "" {
				if err != nil || response.UserID != "user" {
					t.Fatalf("got %+v %v", response, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) || strings.Contains(err.Error(), "credential-do-not-forward") {
				t.Fatalf("got %v", err)
			}
		})
	}
	t.Run("unsupported", func(t *testing.T) {
		if _, err := Call(context.Background(), Request{}); err == nil {
			t.Fatal("accepted unsupported connector")
		}
	})
	t.Run("not installed", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if _, err := Call(context.Background(), Request{Connection: Connection{Plugin: "timetable"}}); err == nil || !strings.Contains(err.Error(), "go install") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("canceled", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "burrowtime-clockify"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := Call(ctx, Request{Connection: Connection{Plugin: "clockify"}}); err == nil {
			t.Fatal("ignored canceled context")
		}
	})
}

func TestLimitedBufferPreservesBound(t *testing.T) {
	t.Parallel()
	var b limitedBuffer
	data := bytes.Repeat([]byte("x"), 8<<20)
	if n, err := b.Write(data); err != nil || n != len(data) {
		t.Fatalf("boundary write %d %v", n, err)
	}
	if n, err := b.Write([]byte("!")); err == nil || n != 0 || b.Len() != len(data) {
		t.Fatalf("overflow write %d %v len %d", n, err, b.Len())
	}
}
