package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fabean/BurrowTime/internal/store"
)

// Exercise the real CLI -> subprocess protocol -> receipt path without making
// external requests or using account credentials.
func TestClockifySubprocessSyncDoesNotDuplicate(t *testing.T) {
	pluginDir := t.TempDir()
	source := filepath.Join(pluginDir, "main.go")
	code := `package main
import("encoding/json";"os")
func main(){
 var r struct{Version int;Operation string};json.NewDecoder(os.Stdin).Decode(&r)
 response:=map[string]any{"version":1}
 switch r.Operation{
 case "check":response["user_id"]="user"
 case "projects":response["projects"]=[]map[string]string{{"id":"project","name":"Portal"}}
 case "create":
  f,e:=os.OpenFile(os.Getenv("BURROWTIME_TEST_POSTS"),os.O_CREATE|os.O_APPEND|os.O_WRONLY,0600);if e!=nil{os.Exit(1)};f.WriteString("create\n");f.Close()
  response["remote_id"]="remote-entry"
 default:response["error"]="unsupported"
 };json.NewEncoder(os.Stdout).Encode(response)
}`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	name := "burrowtime-clockify"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", filepath.Join(pluginDir, name), source).CombinedOutput(); err != nil {
		t.Fatalf("build helper: %s %v", out, err)
	}
	t.Setenv("PATH", pluginDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	posts := filepath.Join(pluginDir, "posts")
	t.Setenv("BURROWTIME_TEST_POSTS", posts)
	dir := t.TempDir()
	t.Setenv("CLOCKIFY_WORKSPACE_ID", "workspace")
	t.Setenv("CLOCKIFY_USER_ID", "user")
	if _, err := runBurrowTimeCommand(dir, "clockify", "configure", "--rounding", "up"); err != nil {
		t.Fatal(err)
	}
	if _, err := runBurrowTimeCommand(dir, "clockify", "map", "portal", "project"); err != nil {
		t.Fatal(err)
	}
	stop := int64(2000)
	frames := []store.Frame{{ID: "one", Start: 1000, Stop: &stop, Project: "portal"}}
	data, _ := json.Marshal(frames)
	if err := os.WriteFile(filepath.Join(dir, "frames"), data, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if out, err := runBurrowTimeCommand(dir, "clockify", "sync", "--all", "--yes"); err != nil {
			t.Fatalf("sync: %s %v", out, err)
		}
	}
	data, err := os.ReadFile(posts)
	if err != nil || string(data) != "create\n" {
		t.Fatalf("duplicate creates: %q %v", data, err)
	}
	status, err := runBurrowTimeCommand(dir, "clockify", "status")
	if err != nil || !strings.Contains(status, "remote-entry") {
		t.Fatalf("missing receipt %s %v", status, err)
	}
}
