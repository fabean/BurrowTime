package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fabean/BurrowTime/internal/watson"
)

func TestConfigCommandReadsWritesAndValidates(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      []string
		want      string
		wantError string
	}{
		{name: "help", want: "configuration options"},
		{name: "set", args: []string{"options.pager", "false"}},
		{name: "get", args: []string{"options.pager"}, want: "false\n"},
		{name: "section", args: []string{"missing.key"}, wantError: "No such section"},
		{name: "option", args: []string{"options.missing"}, wantError: "No such option"},
		{name: "format", args: []string{"missing"}, wantError: "section.option"},
		{name: "empty section", args: []string{".key"}, wantError: "section.option"},
		{name: "empty option", args: []string{"section."}, wantError: "section.option"},
		{name: "too many dots", args: []string{"a.b.c"}, wantError: "section.option"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := runBurrowTimeCommand(dir, "config", "options.pager", "false"); err != nil {
				t.Fatal(err)
			}
			out, err := runBurrowTimeCommand(dir, append([]string{"config"}, tc.args...)...)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("out=%q err=%v", out, err)
				}
			} else if err != nil || !strings.Contains(out, tc.want) {
				t.Fatalf("out=%q err=%v", out, err)
			}
		})
	}
}

func testEditor(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "editor")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", path)
}

func TestConfigEditorSavesOrRestoresValidConfig(t *testing.T) {
	for _, tc := range []struct {
		name      string
		body      string
		want      string
		wantError bool
	}{
		{name: "unchanged", body: ":", want: "true"},
		{name: "empty", body: `: > "$1"`, want: "true"},
		{name: "valid", body: `printf '[options]\npager = false\n' > "$1"`, want: "false"},
		{name: "invalid", body: `printf 'broken\n' > "$1"`, want: "true", wantError: true},
		{name: "editor failure", body: "exit 42", want: "true", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := runBurrowTimeCommand(dir, "config", "options.pager", "true"); err != nil {
				t.Fatal(err)
			}
			testEditor(t, tc.body)
			if out, err := runBurrowTimeCommand(dir, "config", "--edit"); (err != nil) != tc.wantError {
				t.Fatalf("out=%q err=%v", out, err)
			}
			out, err := runBurrowTimeCommand(dir, "config", "options.pager")
			if err != nil || strings.TrimSpace(out) != tc.want {
				t.Fatalf("config=%q err=%v", out, err)
			}
		})
	}
}

func TestRenameUpdatesStoredProjectAndTag(t *testing.T) {
	dir := t.TempDir()
	if _, err := runBurrowTimeCommand(dir, "add", "before", "+old", "--from", "2020-01-01", "--to", "2020-01-02"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		args      []string
		wantError bool
	}{
		{name: "project", args: []string{"project", "before", "after"}},
		{name: "tag", args: []string{"tag", "old", "new"}},
		{name: "bad type", args: []string{"bad", "old", "new"}, wantError: true},
		{name: "unknown", args: []string{"project", "missing", "new"}, wantError: true},
	} {
		out, err := runBurrowTimeCommand(dir, append([]string{"rename"}, tc.args...)...)
		if (err != nil) != tc.wantError {
			t.Fatalf("%s: out=%q err=%v", tc.name, out, err)
		}
	}
	s, err := watson.OpenData(dir)
	if err != nil || s.Frames[0].Project != "after" || len(s.Frames[0].Tags) != 1 || s.Frames[0].Tags[0] != "new" {
		t.Fatalf("service=%+v err=%v", s, err)
	}
}
