package projectconfig

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadNearestAgentConfig(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "one", "two")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	data := "[agent]\nproject = \"sema\"\ntask = \"+SEMA-158\"\nrepository = \"sema-app\"\nlease = \"12m\"\ntask_from_branch = true # infer when task is absent\n"
	if err := os.WriteFile(filepath.Join(root, ".burrowtime.toml"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(nested)
	if err != nil {
		t.Fatal(err)
	}
	if config.Agent.Project != "sema" || config.Agent.Task != "SEMA-158" || config.Agent.Repository != "sema-app" || config.Agent.Lease != 12*time.Minute || !config.Agent.TaskFromBranch {
		t.Fatalf("unexpected config: %#v", config)
	}
}

func TestInvalidAgentConfigReportsLine(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".burrowtime.toml"), []byte("[agent]\nlease = \"later\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("expected invalid lease error")
	}
}
