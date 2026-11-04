package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fabean/BurrowTime/internal/watson"
)

func TestMergeSelectionAndEndOfInput(t *testing.T) {
	for _, tc := range []struct {
		name      string
		input     string
		want      string
		wantError bool
	}{
		{name: "left", input: "L\n", want: "left"},
		{name: "right after invalid", input: "invalid\nr\n", want: "right"},
		{name: "right without newline", input: "r", want: "right"},
		{name: "end of input", want: "left", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "frames"), []byte(`[[10,20,"left","frame",["old"],20]]`), 0600); err != nil {
				t.Fatal(err)
			}
			incoming := filepath.Join(t.TempDir(), "incoming")
			if err := os.WriteFile(incoming, []byte(`[[11,25,"right","frame",["old","new"],25]]`), 0600); err != nil {
				t.Fatal(err)
			}
			out, err := runBurrowTimeCommandInput(dir, tc.input, "merge", incoming, "--force")
			if (err != nil) != tc.wantError || !strings.Contains(out, "frames will need to be resolved") {
				t.Fatalf("out=%q err=%v", out, err)
			}
			s, err := watson.OpenData(dir)
			if err != nil || s.Frames[0].Project != tc.want {
				t.Fatalf("service=%+v err=%v", s, err)
			}
		})
	}
}
