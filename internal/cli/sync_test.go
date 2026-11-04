package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncCommandUsesConfiguredServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("sync omitted credentials")
		}
		if r.Method == http.MethodGet {
			fmt.Fprint(w, "[]")
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	dir := t.TempDir()
	config := "[backend]\nurl = " + server.URL + "\ntoken = test-token\n"
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runBurrowTimeCommand(dir, "sync")
	if err != nil || !strings.Contains(out, "Received 0 frames") || !strings.Contains(out, "Pushed 0 frames") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if _, err := runBurrowTimeCommand(t.TempDir(), "sync"); err == nil {
		t.Fatal("sync without token succeeded")
	}
}
