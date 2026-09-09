package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fabean/BurrowTime/internal/store"
)

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
