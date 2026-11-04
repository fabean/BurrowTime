package watson

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fabean/BurrowTime/internal/store"
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

type syncDoerFunc func(*http.Request) (*http.Response, error)

func (f syncDoerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

type failingSyncReader struct{}

func (failingSyncReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failingSyncReader) Close() error             { return nil }

func TestSyncTransportFailuresAndRemoteValidation(t *testing.T) {
	tests := []struct {
		name, body      string
		status          int
		network, broken bool
	}{
		{name: "network", network: true},
		{name: "server status", status: 500, body: "unavailable"},
		{name: "invalid JSON", status: 200, body: "{"},
		{name: "invalid UUID", status: 200, body: `[{"id":"bad","begin_at":1,"end_at":2}]`},
		{name: "invalid UUID hex", status: 200, body: `[{"id":"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz","begin_at":1,"end_at":2}]`},
		{name: "invalid start", status: 200, body: `[{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","begin_at":{},"end_at":2}]`},
		{name: "invalid stop", status: 200, body: `[{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","begin_at":1,"end_at":"bad"}]`},
		{name: "incomplete body", status: 200, broken: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := testService(t)
			if err := s.Config.Set("backend", "url", "http://sync.invalid"); err != nil {
				t.Fatal(err)
			}
			if err := s.Config.Set("backend", "token", "secret"); err != nil {
				t.Fatal(err)
			}
			client := syncDoerFunc(func(*http.Request) (*http.Response, error) {
				if tt.network {
					return nil, io.ErrUnexpectedEOF
				}
				var body io.ReadCloser = io.NopCloser(strings.NewReader(tt.body))
				if tt.broken {
					body = failingSyncReader{}
				}
				return &http.Response{StatusCode: tt.status, Body: body}, nil
			})
			if _, _, err := s.Sync(client); err == nil {
				t.Fatal("invalid remote response accepted")
			}
			if _, err := os.Stat(filepath.Join(s.Repo.Dir, "last_sync")); !os.IsNotExist(err) {
				t.Fatalf("failed sync checkpoint persisted: %v", err)
			}
		})
	}
	s, _ := testService(t)
	if _, _, err := s.Sync(nil); err == nil {
		t.Fatal("unconfigured sync accepted")
	}
	if err := s.Config.Set("backend", "url", ":bad"); err != nil {
		t.Fatal(err)
	}
	if err := s.Config.Set("backend", "token", "secret"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Sync(nil); err == nil {
		t.Fatal("invalid URL accepted")
	}
	if err := s.Repo.SaveConfig(nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Repo.Dir, "last_sync"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Sync(nil); err == nil {
		t.Fatal("invalid local checkpoint accepted")
	}
}

func TestSyncPreservesLocalMessagesAndSkipsIncompleteFrames(t *testing.T) {
	s, now := testService(t)
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	s.Frames = []store.Frame{{ID: id, Project: "old", Start: 1, Stop: ptr(2), Message: "local", UpdatedAt: 3}, {ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Project: "running", Start: 4, UpdatedAt: 5}}
	s.Messages.Frames[id] = "local"
	if err := s.Config.Set("backend", "url", "http://sync.invalid"); err != nil {
		t.Fatal(err)
	}
	if err := s.Config.Set("backend", "token", "secret"); err != nil {
		t.Fatal(err)
	}
	client := syncDoerFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`[{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","project":"remote","begin_at":"1.5","end_at":"2.9"}]`))}, nil
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "[]" {
			t.Errorf("outgoing=%s", data)
		}
		return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	pulled, pushed, err := s.Sync(client)
	if err != nil || pulled != 1 || pushed != 0 || s.Frames[0].Message != "local" {
		t.Fatalf("pulled=%d pushed=%d frames=%#v err=%v now=%v", pulled, pushed, s.Frames, err, now)
	}
}

func TestSyncPushErrorsAndJSONUnicode(t *testing.T) {
	s, _ := testService(t)
	for _, tt := range []struct {
		name   string
		client HTTPDoer
	}{
		{"network", syncDoerFunc(func(*http.Request) (*http.Response, error) { return nil, io.ErrUnexpectedEOF })},
		{"body", syncDoerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 201, Body: failingSyncReader{}}, nil
		})},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := s.push(tt.client, "http://sync.invalid", "secret", nil); err == nil {
				t.Fatal("push failure ignored")
			}
		})
	}
	if err := s.push(nil, "http://bad\n", "secret", nil); err == nil {
		t.Fatal("invalid push URL accepted")
	}
	for _, id := range []string{"bad", "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"} {
		if _, err := uuidURN(id); err == nil {
			t.Fatal("invalid ID accepted")
		}
	}
	if _, err := remoteTimestamp([]byte(`{}`)); err == nil {
		t.Fatal("invalid timestamp accepted")
	}
	data, err := pythonCompactJSON("café 😀 <&>")
	if err != nil || string(data) != `"caf\u00e9 \ud83d\ude00 <&>"` {
		t.Fatalf("JSON=%s err=%v", data, err)
	}
	if _, err := pythonCompactJSON(make(chan int)); err == nil {
		t.Fatal("unsupported JSON accepted")
	}
	data, err = marshalPushFrames([]pushFrame{{ID: "a", Tags: nonNilTags(nil)}, {ID: "b", Tags: nonNilTags([]string{"tag"})}})
	if err != nil || !strings.Contains(string(data), `"tags": []`) {
		t.Fatalf("body=%s err=%v", data, err)
	}
	for _, body := range []string{"", "null"} {
		client := syncDoerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		frames, err := s.pull(client, "http://sync.invalid", "secret", 0)
		if err != nil || len(frames) != 0 {
			t.Fatalf("empty response=%#v err=%v", frames, err)
		}
	}
}
