package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fabean/BurrowTime/internal/watson"
)

func TestEditParserValidation(t *testing.T) {
	t.Setenv("TZ", "UTC")
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		doc  string
		want string
	}{
		{name: "malformed", doc: `{`, want: "unexpected end"},
		{name: "required", doc: `{}`, want: "must contain"},
		{name: "stop required", doc: `{"project":"p","start":"09:00","tags":[]}`, want: "must contain"},
		{name: "wrong type", doc: `{"project":4,"start":"09:00","stop":"10:00","tags":[]}`, want: "cannot unmarshal"},
		{name: "invalid start", doc: `{"project":"p","start":"invalid","stop":"10:00","tags":[]}`, want: "Invalid time"},
		{name: "invalid stop", doc: `{"project":"p","start":"09:00","stop":"invalid","tags":[]}`, want: "Invalid time"},
		{name: "reversed", doc: `{"project":"p","start":"10:00","stop":"09:00","tags":[]}`, want: "cannot end before"},
		{name: "future stop", doc: `{"project":"p","start":"10:00","stop":"13:00","tags":[]}`, want: "Stop time cannot"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseEditDocument([]byte(tc.doc), true, now); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want %q", err, tc.want)
			}
		})
	}
	if _, err := parseEditDocument([]byte(`{"project":"p","start":"13:00","tags":[]}`), false, now); err == nil || !strings.Contains(err.Error(), "Start time cannot") {
		t.Fatalf("err=%v", err)
	}
	parsed, err := parseEditDocument([]byte(`{"project":"p","start":"09:00","tags":["tag"],"message":""}`), false, now)
	if err != nil || !parsed.MessageSet || parsed.Message != "" || parsed.Start.Hour() != 9 {
		t.Fatalf("parsed=%+v err=%v", parsed, err)
	}
}

func TestEditLegacyFrameRetainsReferenceAndMessage(t *testing.T) {
	t.Setenv("TZ", "UTC")
	for _, tc := range []struct {
		name string
		args []string
		body string
		want string
	}{
		{name: "message flag", args: []string{"edit", "-m", "new message", "--", "-1"}, want: "new message"},
		{name: "editor", args: []string{"edit", "--", "-1"}, body: `printf '%s' '{"project":"edited","start":"2020-01-01 09:00","stop":"2020-01-01 10:00","tags":[],"message":"edited message"}' > "$1"`, want: "edited message"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "frames"), []byte(`[[1577865600,1577869200,"old","",[],1577869200]]`), 0600); err != nil {
				t.Fatal(err)
			}
			if tc.body != "" {
				testEditor(t, tc.body)
			}
			if out, err := runBurrowTimeCommand(dir, tc.args...); err != nil {
				t.Fatalf("out=%q err=%v", out, err)
			}
			s, err := watson.OpenData(dir)
			if err != nil || len(s.Frames) != 1 || s.Frames[0].ID == "" || s.Frames[0].Message != tc.want {
				t.Fatalf("service=%+v err=%v", s, err)
			}
		})
	}
}

func TestEditInvalidDocumentAbortsOnEndOfInput(t *testing.T) {
	dir := t.TempDir()
	if _, err := runBurrowTimeCommand(dir, "start", "project"); err != nil {
		t.Fatal(err)
	}
	testEditor(t, `printf 'invalid' > "$1"`)
	out, err := runBurrowTimeCommand(dir, "edit")
	if err == nil || err.Error() != "Aborted!" || !strings.Contains(out, "Error while parsing") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	s, err := watson.OpenData(dir)
	if err != nil || s.State.Project != "project" {
		t.Fatalf("state=%+v err=%v", s, err)
	}
}

func TestEditorNoChangeAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		body      string
		wantError bool
	}{
		{name: "unchanged", body: ":"},
		{name: "failure", body: "exit 5", wantError: true},
		{name: "removed file", body: `rm "$1"`, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testEditor(t, tc.body)
			_, changed, err := runEditor([]byte("original"), ".json", strings.NewReader(""), io.Discard, io.Discard)
			if (err != nil) != tc.wantError || changed {
				t.Fatalf("changed=%v err=%v", changed, err)
			}
		})
	}
	t.Setenv("EDITOR", "'unterminated")
	if _, _, err := runEditor(nil, ".json", strings.NewReader(""), io.Discard, io.Discard); err == nil {
		t.Fatal("accepted invalid editor")
	}
}
