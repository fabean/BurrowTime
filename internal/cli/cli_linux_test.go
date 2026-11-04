//go:build linux

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fabean/BurrowTime/internal/watson"
	"golang.org/x/sys/unix"
)

// Run the real command in a separate process so terminal modes and Bubble Tea's
// signal handlers cannot affect the test runner.
func TestStartTerminalHelper(t *testing.T) {
	if os.Getenv("BURROWTIME_TEST_TERMINAL") != "1" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv("BURROWTIME_TEST_ARGS")), &args); err != nil {
		t.Fatal(err)
	}
	a := &app{name: os.Getenv("BURROWTIME_TEST_NAME")}
	root := a.root()
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestStartWizardTerminalBehavior(t *testing.T) {
	for _, tc := range []struct {
		name           string
		commandName    string
		args           []string
		disableTUI     bool
		redirectInput  bool
		redirectOutput bool
		wizard         bool
		confirm        bool
		wantProject    string
	}{
		{name: "bare start confirms", wizard: true, confirm: true, wantProject: "new project"},
		{name: "bare start cancels", wizard: true},
		{name: "global flags", args: []string{"--no-color"}, wizard: true},
		{name: "explicit wizard bypasses opt out", args: []string{"-i"}, disableTUI: true, wizard: true},
		{name: "environment opt out", disableTUI: true},
		{name: "Watson compatibility", commandName: "watson"},
		{name: "supplied project", args: []string{"project"}, wantProject: "project"},
		{name: "JSON flag", args: []string{"--json"}},
		{name: "time flag", args: []string{"--at", "09:00"}},
		{name: "false interactive flag", args: []string{"--interactive=false"}},
		{name: "tags without project", args: []string{"+ticket"}},
		{name: "redirected input", redirectInput: true},
		{name: "redirected output", redirectOutput: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			master := os.NewFile(uintptr(fd), "ptmx")
			defer master.Close()
			if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
				t.Fatal(err)
			}
			number, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
			if err != nil {
				t.Fatal(err)
			}
			slave, err := os.OpenFile("/dev/pts/"+strconv.Itoa(number), os.O_RDWR|unix.O_NOCTTY, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer slave.Close()
			if err := unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, &unix.Winsize{Row: 30, Col: 100}); err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			commandName := tc.commandName
			if commandName == "" {
				commandName = "burrowtime"
			}
			args := append([]string{"--watson-dir", dir, "start"}, tc.args...)
			if commandName == "burrowtime" {
				args[0] = "--data-dir"
			}
			encoded, err := json.Marshal(args)
			if err != nil {
				t.Fatal(err)
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestStartTerminalHelper$")
			cmd.Env = append(os.Environ(),
				"BURROWTIME_TEST_TERMINAL=1", "BURROWTIME_TEST_NAME="+commandName,
				"BURROWTIME_TEST_ARGS="+string(encoded), "BURROWTIME_TUI=1", "TERM=xterm-256color",
			)
			if tc.disableTUI {
				cmd.Env = append(cmd.Env, "BURROWTIME_TUI=0")
			}
			cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
			if tc.redirectInput {
				cmd.Stdin = strings.NewReader("")
			}
			output, err := os.CreateTemp(t.TempDir(), "output")
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			if tc.redirectOutput {
				cmd.Stdout, cmd.Stderr = output, output
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill() }()
			if err := slave.Close(); err != nil {
				t.Fatal(err)
			}
			var transcript strings.Builder
			if tc.wizard {
				if err := master.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
					t.Fatal(err)
				}
				buf := make([]byte, 4096)
				for !strings.Contains(transcript.String(), "Type to search or enter a new project") {
					n, err := master.Read(buf)
					transcript.Write(buf[:n])
					if err != nil {
						t.Fatalf("start wizard did not appear: %v\n%s", err, transcript.String())
					}
				}
				keys := "\x03"
				if tc.confirm {
					keys = "new project\r\r\rnotes\r\r"
				}
				if _, err := master.Write([]byte(keys)); err != nil {
					t.Fatal(err)
				}
			}
			readDone := make(chan struct{})
			go func() {
				_, _ = io.Copy(&transcript, master)
				close(readDone)
			}()
			err = cmd.Wait()
			<-readDone
			wantSuccess := tc.wantProject != ""
			if (err == nil) != wantSuccess || ctx.Err() != nil {
				t.Fatalf("command result=%v context=%v\n%s", err, ctx.Err(), transcript.String())
			}
			service, err := watson.OpenData(dir)
			if err != nil {
				t.Fatal(err)
			}
			if service.State.Project != tc.wantProject || len(service.Frames) != 0 {
				t.Fatalf("state=%+v frames=%v", service.State, service.Frames)
			}
			if tc.confirm && service.Messages.Timers[watson.PrimaryTimerID] != "notes" {
				t.Fatalf("wizard message not saved: %+v", service.Messages)
			}
			if !wantSuccess {
				text := transcript.String()
				if tc.redirectOutput {
					data, err := os.ReadFile(output.Name())
					if err != nil {
						t.Fatal(err)
					}
					text = string(data)
				}
				wantError := "No project given."
				if tc.wizard {
					wantError = "Aborted!"
				}
				if !strings.Contains(text, wantError) {
					t.Fatalf("output=%q, want %q", text, wantError)
				}
				if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
					t.Fatalf("canceled or invalid start wrote timer state: %v", err)
				}
			}
		})
	}
}
