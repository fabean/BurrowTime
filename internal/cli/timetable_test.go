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

func TestTimetableConfigureMapAndDryRun(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TIMETABLE_TOKEN", "never-save-this")
	if _, err := runBurrowTimeCommand(dir, "timetable", "configure", "--user", "user-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := runBurrowTimeCommand(dir, "timetable", "map", "portal", "project-1"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "integrations.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("never-save-this")) {
		t.Fatal("saved personal token")
	}
	var config integrations.Config
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.Routes["timetable"]["portal"].ProjectID != "project-1" {
		t.Fatal("missing Timetable mapping")
	}
	stop := int64(1960)
	frames := []store.Frame{{ID: "frame-1", Start: 1000, Stop: &stop, Project: "portal"}}
	data, _ = json.Marshal(frames)
	if err := os.WriteFile(filepath.Join(dir, "frames"), data, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runBurrowTimeCommand(dir, "timetable", "sync", "--all", "--dry-run")
	if err != nil || !strings.Contains(out, "export 16m0s") {
		t.Fatalf("dry run: %s %v", out, err)
	}
	_, err = runBurrowTimeCommand(dir, "timetable", "sync", "--all")
	if err == nil || !strings.Contains(err.Error(), "confirmation") {
		t.Fatalf("missing approval: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "integration-sync.json")); !os.IsNotExist(err) {
		t.Fatal("saved receipt before confirmation")
	}
}
