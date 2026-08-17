package watson

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/josh/burrowtime/internal/store"
)

func TestSyncWireProtocolAndStateTransition(t *testing.T) {
	t.Setenv("TZ", "UTC")
	var gotPost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token secret" {
			t.Errorf("authorization=%q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type=%q", r.Header.Get("Content-Type"))
		}
		switch r.URL.Path {
		case "/frames/":
			if r.Method != http.MethodGet {
				t.Errorf("method=%s", r.Method)
			}
			if got := r.URL.Query().Get("last_sync"); got != "1970-01-01T00:01:40+00:00" {
				t.Errorf("last_sync=%q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `[{"id":"11111111-1111-1111-1111-111111111111","project":"remote replacement","begin_at":120,"end_at":130,"tags":["remote"]},{"id":"33333333-3333-3333-3333-333333333333","project":"remote new","begin_at":"1970-01-01T00:02:20+00:00","end_at":"1970-01-01T00:02:30+00:00","tags":[]}]`)
		case "/frames/bulk/":
			if r.Method != http.MethodPost {
				t.Errorf("method=%s", r.Method)
			}
			body, _ := io.ReadAll(r.Body)
			gotPost = string(body)
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	config := "[backend]\nurl = " + server.URL + "\ntoken = secret\n"
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	repo, _ := store.New(dir)
	stop1, stop2 := int64(20), int64(40)
	frames := []store.Frame{{Start: 10, Stop: &stop1, Project: "old", ID: "11111111111111111111111111111111", Tags: []string{}, UpdatedAt: 50}, {Start: 30, Stop: &stop2, Project: "local", ID: "22222222222222222222222222222222", Tags: []string{"tag"}, UpdatedAt: 150}}
	if err := repo.SaveFrames(frames); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveLastSync(100); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return time.Unix(200, 0).UTC() }
	pulled, pushed, err := s.Sync(server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if pulled != 2 || pushed != 1 {
		t.Fatalf("pulled=%d pushed=%d", pulled, pushed)
	}
	wantPost := `[{"id": "urn:uuid:22222222-2222-2222-2222-222222222222", "begin_at": "1970-01-01T00:00:30+00:00", "end_at": "1970-01-01T00:00:40+00:00", "project": "local", "tags": ["tag"]}]`
	if gotPost != wantPost {
		t.Fatalf("post body\nwant %s\n got %s", wantPost, gotPost)
	}
	loaded, err := repo.LoadFrames()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 3 || loaded[0].Project != "remote replacement" || loaded[2].Project != "remote new" {
		t.Fatalf("frames=%#v", loaded)
	}
	last, err := repo.LoadLastSync()
	if err != nil {
		t.Fatal(err)
	}
	if last != 200 {
		t.Fatalf("last_sync=%d", last)
	}
}

func TestFailedPushDoesNotPersistPulledFrames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `[{"id":"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa","project":"remote","begin_at":1,"end_at":2,"tags":[]}]`)
			return
		}
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	defer server.Close()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config"), []byte("[backend]\nurl = "+server.URL+"\ntoken = secret\n"), 0o600)
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Now = func() time.Time { return time.Unix(100, 0).UTC() }
	if _, _, err = s.Sync(server.Client()); err == nil {
		t.Fatal("expected sync error")
	}
	repo, _ := store.New(dir)
	frames, err := repo.LoadFrames()
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 0 {
		t.Fatalf("failed sync persisted frames: %#v", frames)
	}
	if _, err := os.Stat(filepath.Join(dir, "last_sync")); !os.IsNotExist(err) {
		t.Fatalf("last_sync created on failure: %v", err)
	}
}
