package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// TestPythonWatsonOracle runs when the frozen Watson checkout is available.
// It makes machine-readable output drift visible while remaining skippable in
// an isolated downstream build.
func TestPythonWatsonOracle(t *testing.T) {
	oracle := "/home/josh/Projects/Watson"
	if _, err := os.Stat(filepath.Join(oracle, "watson", "__main__.py")); err != nil {
		t.Skip("local Python Watson oracle is not available")
	}
	dir := t.TempDir()
	frames := `[
 [1549962000, 1549965600, "café <&> 😀", "aaaaaaaa111111111111111111111111", ["täg"], 1549965600],
 [1550048400, 1550050200, "beta", "bbbbbbbb222222222222222222222222", ["two", "one"], 1550050200]
]`
	if err := os.WriteFile(filepath.Join(dir, "frames"), []byte(frames), 0o600); err != nil {
		t.Fatal(err)
	}

	oldLocal := time.Local
	time.Local = time.UTC
	defer func() { time.Local = oldLocal }()

	cases := [][]string{
		{"projects"},
		{"tags"},
		{"frames"},
		{"log", "--from", "2019-02-12", "--to", "2019-02-13", "--csv"},
		{"log", "--from", "2019-02-12", "--to", "2019-02-13", "--json"},
		{"report", "--from", "2019-02-12", "--to", "2019-02-13", "--csv"},
		{"report", "--from", "2019-02-12", "--to", "2019-02-13", "--json"},
		{"aggregate", "--from", "2019-02-12", "--to", "2019-02-13", "--csv"},
		{"aggregate", "--from", "2019-02-12", "--to", "2019-02-13", "--json"},
		{"log", "--luna", "--csv"},
		{"report", "--luna", "--json"},
	}
	for _, args := range cases {
		t.Run(args[0]+args[len(args)-1], func(t *testing.T) {
			python := exec.Command("python", append([]string{"-m", "watson"}, args...)...)
			python.Dir = oracle
			python.Env = append(os.Environ(), "WATSON_DIR="+dir, "TZ=UTC")
			want, err := python.CombinedOutput()
			if err != nil {
				t.Fatalf("Python Watson: %v\n%s", err, want)
			}

			var got bytes.Buffer
			a := &app{in: os.Stdin, out: os.Stdout, errOut: os.Stderr}
			root := a.root()
			root.SetArgs(append([]string{"--watson-dir", dir}, args...))
			root.SetOut(&got)
			root.SetErr(&got)
			if err := root.Execute(); err != nil {
				t.Fatalf("BurrowTime: %v\n%s", err, got.String())
			}
			if !bytes.Equal(got.Bytes(), want) {
				t.Fatalf("output differs\nPython:\n%s\nBurrowTime:\n%s", want, got.Bytes())
			}
		})
	}
}

func TestPythonWatsonMergeOracle(t *testing.T) {
	oracle := requireOracle(t)
	seed := `[[10,20,"original","11111111111111111111111111111111",[],20]]`
	incoming := `[[30,40,"incoming","22222222222222222222222222222222",["tag"],40]]`
	pythonDir, goDir := t.TempDir(), t.TempDir()
	for _, dir := range []string{pythonDir, goDir} {
		if err := os.WriteFile(filepath.Join(dir, "frames"), []byte(seed), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pythonIncoming := filepath.Join(pythonDir, "incoming")
	goIncoming := filepath.Join(goDir, "incoming")
	os.WriteFile(pythonIncoming, []byte(incoming), 0o600)
	os.WriteFile(goIncoming, []byte(incoming), 0o600)
	python := exec.Command("python", "-m", "watson", "merge", pythonIncoming, "--force")
	python.Dir = oracle
	python.Env = append(os.Environ(), "WATSON_DIR="+pythonDir, "TZ=UTC")
	want, err := python.CombinedOutput()
	if err != nil {
		t.Fatalf("Python merge: %v\n%s", err, want)
	}
	got, err := runGoOracle(goDir, []string{"merge", goIncoming, "--force"})
	if err != nil {
		t.Fatalf("Go merge: %v\n%s", err, got)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("merge output differs\nPython:\n%s\nGo:\n%s", want, got)
	}
	pythonFrames, _ := os.ReadFile(filepath.Join(pythonDir, "frames"))
	goFrames, _ := os.ReadFile(filepath.Join(goDir, "frames"))
	if !bytes.Equal(pythonFrames, goFrames) {
		t.Fatalf("merged files differ\nPython:\n%s\nGo:\n%s", pythonFrames, goFrames)
	}
}

type capturedRequest struct{ method, path, query, authorization, contentType, body string }

func TestPythonWatsonSyncWireOracle(t *testing.T) {
	oracle := requireOracle(t)
	captures := []capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captures = append(captures, capturedRequest{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, authorization: r.Header.Get("Authorization"), contentType: r.Header.Get("Content-Type"), body: string(body)})
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, "[]")
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	seed := `[[10,20,"sync café <&> 😀","11111111111111111111111111111111",["täg"],100]]`
	config := "[backend]\nurl = " + server.URL + "\ntoken = secret\n"
	pythonDir, goDir := t.TempDir(), t.TempDir()
	for _, dir := range []string{pythonDir, goDir} {
		os.WriteFile(filepath.Join(dir, "frames"), []byte(seed), 0o600)
		os.WriteFile(filepath.Join(dir, "last_sync"), []byte("0"), 0o600)
		os.WriteFile(filepath.Join(dir, "config"), []byte(config), 0o600)
	}
	python := exec.Command("python", "-m", "watson", "sync")
	python.Dir = oracle
	python.Env = append(os.Environ(), "WATSON_DIR="+pythonDir, "TZ=UTC")
	want, err := python.CombinedOutput()
	if err != nil {
		t.Fatalf("Python sync: %v\n%s", err, want)
	}
	got, err := runGoOracle(goDir, []string{"sync"})
	if err != nil {
		t.Fatalf("Go sync: %v\n%s", err, got)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("sync output differs\nPython:\n%s\nGo:\n%s", want, got)
	}
	if len(captures) != 4 {
		t.Fatalf("captured %d requests: %#v", len(captures), captures)
	}
	for i := 0; i < 2; i++ {
		left, right := captures[i], captures[i+2]
		if left != right {
			t.Fatalf("wire request %d differs\nPython: %#v\nGo: %#v", i, left, right)
		}
	}
}

func TestPythonWatsonEditOracle(t *testing.T) {
	oracle := requireOracle(t)
	seed := `[[1549962000,1549965600,"old","11111111111111111111111111111111",[],1549965600]]`
	pythonDir, goDir := t.TempDir(), t.TempDir()
	for _, dir := range []string{pythonDir, goDir} {
		os.WriteFile(filepath.Join(dir, "frames"), []byte(seed), 0o600)
	}
	script := filepath.Join(t.TempDir(), "editor")
	body := "#!/bin/sh\nprintf '%s' '{\"project\":\"edited\",\"start\":\"2019-02-12 11:00:00\",\"stop\":\"2019-02-12 12:00:00\",\"tags\":[\"new\"]}' > \"$1\"\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	python := exec.Command("python", "-m", "watson", "edit", "-1")
	python.Dir = oracle
	python.Env = append(os.Environ(), "WATSON_DIR="+pythonDir, "TZ=UTC", "VISUAL=", "EDITOR="+script)
	want, err := python.CombinedOutput()
	if err != nil {
		t.Fatalf("Python edit: %v\n%s", err, want)
	}
	t.Setenv("TZ", "UTC")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", script)
	got, err := runGoOracle(goDir, []string{"edit", "-1"})
	if err != nil {
		t.Fatalf("Go edit: %v\n%s", err, got)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("edit output differs\nPython:\n%s\nGo:\n%s", want, got)
	}
	var pythonRows, goRows [][]any
	pythonData, _ := os.ReadFile(filepath.Join(pythonDir, "frames"))
	goData, _ := os.ReadFile(filepath.Join(goDir, "frames"))
	if err := json.Unmarshal(pythonData, &pythonRows); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(goData, &goRows); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(pythonRows[0][:5], goRows[0][:5]) {
		t.Fatalf("edited frame differs\nPython: %#v\nGo: %#v", pythonRows[0], goRows[0])
	}
}

func TestPythonWatsonConfigRewriteOracle(t *testing.T) {
	oracle := requireOracle(t)
	seed := "[DEFAULT]\nFoo = base\n\n[Mixed]\nBar = first\n second\nempty =\n  one\n  two\n"
	pythonDir, goDir := t.TempDir(), t.TempDir()
	for _, dir := range []string{pythonDir, goDir} {
		if err := os.WriteFile(filepath.Join(dir, "config"), []byte(seed), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	python := exec.Command("python", "-m", "watson", "config", "Mixed.New", "value")
	python.Dir = oracle
	python.Env = append(os.Environ(), "WATSON_DIR="+pythonDir, "TZ=UTC")
	wantOutput, err := python.CombinedOutput()
	if err != nil {
		t.Fatalf("Python config: %v\n%s", err, wantOutput)
	}
	gotOutput, err := runGoOracle(goDir, []string{"config", "Mixed.New", "value"})
	if err != nil {
		t.Fatalf("Go config: %v\n%s", err, gotOutput)
	}
	if !bytes.Equal(wantOutput, gotOutput) {
		t.Fatalf("config output differs\nPython: %q\nGo: %q", wantOutput, gotOutput)
	}
	for _, name := range []string{"config", "config.bak"} {
		want, readErr := os.ReadFile(filepath.Join(pythonDir, name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		got, readErr := os.ReadFile(filepath.Join(goDir, name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(want, got) {
			t.Fatalf("%s differs\nPython: %q\nGo: %q", name, want, got)
		}
	}
}

func TestPythonAndGoAlternateOnOneDirectory(t *testing.T) {
	oracle := requireOracle(t)
	dir := t.TempDir()
	runPython := func(args ...string) []byte {
		t.Helper()
		command := exec.Command("python", append([]string{"-m", "watson"}, args...)...)
		command.Dir = oracle
		command.Env = append(os.Environ(), "WATSON_DIR="+dir, "TZ=UTC")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("Python Watson %v: %v\n%s", args, err, output)
		}
		return output
	}
	runGo := func(args ...string) []byte {
		t.Helper()
		t.Setenv("TZ", "UTC")
		output, err := runGoOracle(dir, args)
		if err != nil {
			t.Fatalf("Go Watson %v: %v\n%s", args, err, output)
		}
		return output
	}

	runPython("start", "--at", "2019-02-12 10:00", "python writer", "+one")
	if got := string(runGo("status", "--project")); got != "python writer\n" {
		t.Fatalf("Go did not read Python state: %q", got)
	}
	runGo("stop", "--at", "2019-02-12 11:00")
	if output := runPython("log", "--all", "--json"); !bytes.Contains(output, []byte(`"project": "python writer"`)) {
		t.Fatalf("Python did not read Go frame: %s", output)
	}
	runGo("start", "--at", "2019-02-12 12:00", "go writer", "+two")
	runPython("stop", "--at", "2019-02-12 13:00")
	output := runGo("log", "--all", "--json")
	var frames []logJSONFrame
	if err := json.Unmarshal(output, &frames); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || frames[0].Project != "python writer" || frames[1].Project != "go writer" {
		t.Fatalf("alternating history differs: %#v", frames)
	}
}

func requireOracle(t *testing.T) string {
	t.Helper()
	oracle := "/home/josh/Projects/Watson"
	if _, err := os.Stat(filepath.Join(oracle, "watson", "__main__.py")); err != nil {
		t.Skip("local Python Watson oracle is not available")
	}
	return oracle
}
func runGoOracle(dir string, args []string) ([]byte, error) {
	var output bytes.Buffer
	a := &app{}
	root := a.root()
	root.SetArgs(normalizeNegativeFrameArgs(append([]string{"--watson-dir", dir}, args...)))
	root.SetOut(&output)
	root.SetErr(&output)
	err := root.Execute()
	return output.Bytes(), err
}
