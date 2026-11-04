package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fabean/BurrowTime/internal/integrations"
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
 var r struct{Version int;Operation string;Entry struct{Billable bool}};json.NewDecoder(os.Stdin).Decode(&r)
 response:=map[string]any{"version":1}
 switch r.Operation{
 case "check":response["user_id"]="user"
 case "projects":response["projects"]=[]map[string]string{{"id":"project","name":"Portal"}}
 case "create":
  if !r.Entry.Billable{json.NewEncoder(os.Stdout).Encode(map[string]any{"version":1,"error":"expected billable entry"});return}
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

func TestExporterConnectorProjectsAndPendingRecovery(t *testing.T) {
	pluginDir := t.TempDir()
	source := filepath.Join(pluginDir, "main.go")
	code := `package main
import("encoding/json";"os")
func main(){var r struct{Operation string;RemoteID string ` + "`json:\"remote_id\"`" + `;Entry json.RawMessage};json.NewDecoder(os.Stdin).Decode(&r);response:=map[string]any{"version":1};if os.Getenv("TEST_EXPORT_FAIL")==r.Operation{response["error"]="fixture failure"}else{switch r.Operation{case "check":response["user_id"]="user";case "projects":response["projects"]=[]map[string]string{{"id":"project","name":"Portal","client_name":"Client"}};case "create":response["remote_id"]="created";case "get":response["remote_id"]=r.RemoteID;response["user_id"]="user";response["entry"]=json.RawMessage(os.Getenv("TEST_EXPORT_ENTRY"))}};json.NewEncoder(os.Stdout).Encode(response)}`
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"clockify", "timetable"} {
		name := "burrowtime-" + provider
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if out, err := exec.Command("go", "build", "-o", filepath.Join(pluginDir, name), source).CombinedOutput(); err != nil {
			t.Fatalf("build fixture %s %v", out, err)
		}
	}
	t.Setenv("PATH", pluginDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, provider := range []string{"clockify", "timetable"} {
		t.Run(provider, func(t *testing.T) {
			dir := t.TempDir()
			args := []string{provider, "configure", "--user", "user"}
			if provider == "clockify" {
				args = append(args, "--workspace", "workspace")
			}
			if _, err := runBurrowTimeCommand(dir, args...); err != nil {
				t.Fatal(err)
			}
			for _, operation := range []string{"check", "projects", ""} {
				t.Run("projects "+operation, func(t *testing.T) {
					t.Setenv("TEST_EXPORT_FAIL", operation)
					out, err := runBurrowTimeCommand(dir, provider, "projects")
					if operation != "" {
						if err == nil || !strings.Contains(err.Error(), "fixture failure") {
							t.Fatalf("got %s %v", out, err)
						}
					} else if err != nil || !strings.Contains(out, "project\tPortal\tClient") {
						t.Fatalf("got %s %v", out, err)
					}
				})
			}
			if _, err := runBurrowTimeCommand(dir, provider, "map", "portal", "project"); err != nil {
				t.Fatal(err)
			}
			stop := int64(2000)
			repo, err := store.New(dir)
			if err != nil {
				t.Fatal(err)
			}
			frames := []store.Frame{{ID: "frame", Start: 1000, Stop: &stop, Project: "portal"}}
			if err := repo.SaveFrames(frames); err != nil {
				t.Fatal(err)
			}
			c, err := integrations.LoadConfig(dir)
			if err != nil {
				t.Fatal(err)
			}
			mapping, ok := c.Mapping(provider, "portal")
			if !ok {
				t.Fatal("missing map")
			}
			r, err := integrations.Projection(frames[0], provider, c.Connections[provider], mapping)
			if err != nil {
				t.Fatal(err)
			}
			key := "frame"
			if provider == "timetable" {
				key = provider + ":" + key
			}
			ledger := integrations.Ledger{Version: 1, Records: map[string]integrations.Receipt{key: r}}
			if err := integrations.SaveLedger(dir, ledger); err != nil {
				t.Fatal(err)
			}
			out, err := runBurrowTimeCommand(dir, provider, "status")
			if err != nil || !strings.Contains(out, "pending") {
				t.Fatalf("pending status %s %v", out, err)
			}
			if provider == "clockify" {
				if _, err := runBurrowTimeCommand(dir, provider, "retry", "frame"); err == nil {
					t.Fatal("retry did not require explicit confirmation")
				}
				if _, err := runBurrowTimeCommand(dir, provider, "retry", "missing", "--confirmed-not-created"); err == nil {
					t.Fatal("retried missing receipt")
				}
				data, err := json.Marshal(r.Entry)
				if err != nil {
					t.Fatal(err)
				}
				t.Setenv("TEST_EXPORT_ENTRY", string(data))
				if out, err := runBurrowTimeCommand(dir, provider, "resolve", "frame", "remote"); err != nil || !strings.Contains(out, "marked synced") {
					t.Fatalf("resolve %s %v", out, err)
				}
				if _, err := runBurrowTimeCommand(dir, provider, "resolve", "missing", "remote"); err == nil {
					t.Fatal("resolved missing receipt")
				}
				if err := integrations.SaveLedger(dir, ledger); err != nil {
					t.Fatal(err)
				}
				if out, err := runBurrowTimeCommand(dir, provider, "retry", "frame", "--confirmed-not-created"); err != nil || !strings.Contains(out, "eligible for retry") {
					t.Fatalf("retry %s %v", out, err)
				}
			}
			if out, err := runBurrowTimeCommand(dir, provider, "sync", "--all", "--yes"); err != nil || !strings.Contains(out, "1 created") {
				t.Fatalf("sync %s %v", out, err)
			}
			ledger, err = integrations.LoadLedger(dir)
			if err != nil || ledger.Records[key].Status != "synced" {
				t.Fatalf("receipt %+v %v", ledger, err)
			}
		})
	}
	t.Run("unconfigured recovery", func(t *testing.T) {
		for _, args := range [][]string{{"resolve", "frame", "remote"}, {"retry", "frame", "--confirmed-not-created"}} {
			if _, err := runBurrowTimeCommand(t.TempDir(), append([]string{"clockify"}, args...)...); err == nil || !strings.Contains(err.Error(), "not configured") {
				t.Fatalf("got %v", err)
			}
		}
	})
}
