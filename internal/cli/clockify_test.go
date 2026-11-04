package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fabean/BurrowTime/internal/integrations"
	"github.com/fabean/BurrowTime/internal/store"
)

func TestClockifyMapBillable(t *testing.T) {
	for _, tt := range []struct {
		name  string
		flags []string
		want  bool
	}{
		{name: "default", flags: []string{}, want: true},
		{name: "explicit billable", flags: []string{"--billable"}, want: true},
		{name: "non-billable", flags: []string{"--billable=false"}, want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("CLOCKIFY_WORKSPACE_ID", "workspace")
			t.Setenv("CLOCKIFY_USER_ID", "user")
			if _, err := runBurrowTimeCommand(dir, "clockify", "configure"); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"clockify", "map", "portal", "project"}, tt.flags...)
			if _, err := runBurrowTimeCommand(dir, args...); err != nil {
				t.Fatal(err)
			}
			config, err := integrations.LoadConfig(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := config.Projects["portal"].Billable; got != tt.want {
				t.Fatalf("saved billable = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClockifyConfigureMapDryRunAndConfirmation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLOCKIFY_WORKSPACE_ID", "workspace")
	t.Setenv("CLOCKIFY_USER_ID", "user")
	t.Setenv("CLOCKIFY_API_KEY", "never-save-this")
	if _, err := runBurrowTimeCommand(dir, "clockify", "configure", "--rounding", "up", "--increment", "15m"); err != nil {
		t.Fatal(err)
	}
	if _, err := runBurrowTimeCommand(dir, "clockify", "map", "portal", "project"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "integrations.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("never-save-this")) {
		t.Fatal("saved API key")
	}
	stop := int64(1960)
	frames := []store.Frame{{ID: "frame-1", Start: 1000, Stop: &stop, Project: "portal", Tags: []string{"PORTAL-42"}}}
	data, _ = json.Marshal(frames)
	if err := os.WriteFile(filepath.Join(dir, "frames"), data, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runBurrowTimeCommand(dir, "clockify", "sync", "--all", "--dry-run")
	if err != nil || !strings.Contains(out, "export 30m0s") {
		t.Fatalf("%s %v", out, err)
	}
	_, err = runBurrowTimeCommand(dir, "clockify", "sync", "--all")
	if err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Fatalf("missing approval: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "integration-sync.json")); !os.IsNotExist(err) {
		t.Fatal("saved receipt before confirmation")
	}
	if _, err := runBurrowTimeCommand(dir, "clockify", "configure", "--increment", "0s"); err == nil {
		t.Fatal("accepted invalid rounding")
	}
	if _, err := runBurrowTimeCommand(dir, "clockify", "sync", "--all", "--today"); err == nil {
		t.Fatal("accepted conflicting dates")
	}
}

func TestClockifyRejectsAnotherProvidersConnection(t *testing.T) {
	dir := t.TempDir()
	if _, err := runBurrowTimeCommand(dir, "timetable", "configure", "--user", "user"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "integrations.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"configure", "--workspace", "workspace"}, {"map", "portal", "p"}, {"status"}, {"sync", "--all", "--dry-run"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			command := append([]string{"clockify", "--connection", "timetable"}, args...)
			if _, err := runBurrowTimeCommand(dir, command...); err == nil || !strings.Contains(err.Error(), "belongs to timetable") {
				t.Fatalf("got %v", err)
			}
		})
	}
	after, err := os.ReadFile(filepath.Join(dir, "integrations.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("mutated another provider's config")
	}
}

func TestExportCommandValidationAndDates(t *testing.T) {
	for _, provider := range []string{"clockify", "timetable"} {
		t.Run(provider, func(t *testing.T) {
			configure := []string{provider, "configure", "--user", "user"}
			if provider == "clockify" {
				configure = append(configure, "--workspace", "workspace")
			}
			for _, tc := range []struct {
				name       string
				args       []string
				want       string
				configured bool
			}{
				{name: "empty connection", args: []string{"configure", "--connection", ""}, want: "connection name"},
				{name: "missing user", args: []string{"configure", "--user", ""}, want: "required"},
				{name: "missing project config", args: []string{"projects"}, want: "configure connection"},
				{name: "missing map config", args: []string{"map", "local", "remote"}, want: "configure connection"},
				{name: "empty local", args: []string{"map", "", "remote"}, want: "cannot be empty", configured: true},
				{name: "empty remote", args: []string{"map", "local", ""}, want: "cannot be empty", configured: true},
				{name: "map rounding", args: []string{"map", "local", "remote", "--rounding", "invalid"}, want: "rounding mode", configured: true},
				{name: "map increment", args: []string{"map", "local", "remote", "--rounding", "up", "--increment", "0s"}, want: "increment", configured: true},
				{name: "conflicting today", args: []string{"sync", "--today", "--from", "2026-01-01"}, want: "choose", configured: true},
				{name: "conflicting all", args: []string{"sync", "--all", "--today"}, want: "choose", configured: true},
				{name: "missing to", args: []string{"sync", "--from", "2026-01-01"}, want: "both", configured: true},
				{name: "bad from", args: []string{"sync", "--from", "bad", "--to", "2026-01-01"}, want: "parsing time", configured: true},
				{name: "bad to", args: []string{"sync", "--from", "2026-01-01", "--to", "bad"}, want: "parsing time", configured: true},
				{name: "reversed dates", args: []string{"sync", "--from", "2026-01-02", "--to", "2026-01-01"}, want: "must not precede", configured: true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					dir := t.TempDir()
					if tc.configured {
						if _, err := runBurrowTimeCommand(dir, configure...); err != nil {
							t.Fatal(err)
						}
					}
					args := append([]string{provider}, tc.args...)
					if _, err := runBurrowTimeCommand(dir, args...); err == nil || !strings.Contains(err.Error(), tc.want) {
						t.Fatalf("got %v want %q", err, tc.want)
					}
				})
			}
			t.Run("updated connection and map", func(t *testing.T) {
				dir := t.TempDir()
				if _, err := runBurrowTimeCommand(dir, configure...); err != nil {
					t.Fatal(err)
				}
				args := []string{provider, "configure", "--user", "new-user", "--rounding", "nearest", "--increment", "5m"}
				if provider == "clockify" {
					args = append(args, "--workspace", "new-workspace", "--api-key-env", "CUSTOM_KEY")
				} else {
					args = append(args, "--url", "https://custom.test", "--token-env", "CUSTOM_KEY")
				}
				if _, err := runBurrowTimeCommand(dir, args...); err != nil {
					t.Fatal(err)
				}
				if _, err := runBurrowTimeCommand(dir, provider, "map", "portal", "project", "--rounding", "up", "--increment", "10m"); err != nil {
					t.Fatal(err)
				}
				c, err := integrations.LoadConfig(dir)
				if err != nil {
					t.Fatal(err)
				}
				if c.Connections[provider].UserID != "new-user" || c.Connections[provider].APIKeyEnv != "CUSTOM_KEY" {
					t.Fatalf("did not update connection %+v", c.Connections[provider])
				}
				m, ok := c.Mapping(provider, "portal")
				if !ok || m.Rounding == nil || m.Rounding.Mode != "up" || m.Rounding.Increment != "10m" {
					t.Fatalf("wrong map %+v", m)
				}
				for _, args := range [][]string{{"sync", "--dry-run"}, {"sync", "--dry-run", "--from", "2026-01-01", "--to", "2026-01-02"}, {"status"}} {
					if out, err := runBurrowTimeCommand(dir, append([]string{provider}, args...)...); err != nil {
						t.Fatalf("%s: %s %v", args, out, err)
					}
				}
			})
			t.Run("invalid files", func(t *testing.T) {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "integrations.json"), []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := runBurrowTimeCommand(dir, provider, "status"); err == nil {
					t.Fatal("accepted corrupt config")
				}
				if err := os.Remove(filepath.Join(dir, "integrations.json")); err != nil {
					t.Fatal(err)
				}
				unlock, err := integrations.Lock(dir)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := runBurrowTimeCommand(dir, provider, "status"); err == nil {
					t.Fatal("ignored lock")
				}
				unlock()
				if _, err := runBurrowTimeCommand(dir, configure...); err != nil {
					t.Fatal(err)
				}
				for _, file := range []string{"integration-sync.json", "frames"} {
					if err := os.WriteFile(filepath.Join(dir, file), []byte("{"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				for _, command := range []string{"status", "sync"} {
					if _, err := runBurrowTimeCommand(dir, provider, command); err == nil {
						t.Fatalf("accepted corrupt %s data", command)
					}
				}
			})
		})
	}
}
