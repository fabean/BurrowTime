package integrations

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigDefaultsRoundTripAndInvalidState(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c, err := LoadConfig(dir)
	if err != nil || c.Version != 1 || c.Connections == nil || c.Projects == nil {
		t.Fatalf("defaults %+v %v", c, err)
	}
	c.Connections["work"] = Connection{Plugin: "clockify", WorkspaceID: "w", UserID: "u", APIKeyEnv: "KEY"}
	if err := SaveConfig(dir, c); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(dir)
	if err != nil || loaded.Connections["work"] != c.Connections["work"] {
		t.Fatalf("round trip %+v %v", loaded, err)
	}
	for _, tc := range []struct{ name, data, want string }{{name: "malformed", data: "{", want: "invalid integrations.json"}, {name: "schema", data: `{"version":2,"connections":{},"projects":{}}`, want: "schema"}, {name: "missing maps", data: `{"version":1}`, want: "schema"}} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(dir, "integrations.json"), []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(dir); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v", err)
			}
		})
	}
	if err := os.Remove(filepath.Join(dir, "integrations.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "integrations.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(dir); err == nil {
		t.Fatal("accepted directory as config")
	}
}

func TestStateWritesFailWithoutReplacingExistingData(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "state")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := write(path, make(chan int)); err == nil {
		t.Fatal("accepted unencodable state")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Fatalf("lost original %q %v", data, err)
	}
	if err := SaveConfig(filepath.Join(path, "nested"), Config{}); err == nil {
		t.Fatal("accepted file as parent directory")
	}
	if _, err := Lock(path); err == nil {
		t.Fatal("locked file as directory")
	}
	destination := filepath.Join(dir, "destination")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	if err := write(destination, Config{}); err == nil {
		t.Fatal("replaced directory with file")
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".integration-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("leaked temporary files %v %v", matches, err)
	}
}

func TestRetryPendingPropagatesUnreadableLedger(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "integration-sync.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RetryPending(dir, "work", "f", Connection{}); err == nil {
		t.Fatal("ignored corrupt ledger")
	}
	if _, err := os.Stat(filepath.Join(dir, "integration-sync.json")); errors.Is(err, os.ErrNotExist) {
		t.Fatal("removed corrupt ledger")
	}
}
