package integrations

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fabean/BurrowTime/internal/store"
)

func fixture() (Config, []store.Frame) {
	stop := int64(1960)
	c := Config{Version: 1, Connections: map[string]Connection{"work": {Plugin: "clockify", WorkspaceID: "workspace", UserID: "user", APIKeyEnv: "CLOCKIFY_API_KEY", Rounding: Rounding{Mode: "up", Increment: "15m"}}}, Projects: map[string]Mapping{"portal": {Connection: "work", ProjectID: "project"}}}
	return c, []store.Frame{{ID: "frame-1", Start: 1000, Stop: &stop, Project: "portal", Tags: []string{"PORTAL-42"}}}
}

func mockCall(creates *int) Caller {
	return func(_ context.Context, r Request) (Response, error) {
		switch r.Operation {
		case "check":
			return Response{UserID: "user"}, nil
		case "projects":
			return Response{Projects: []Project{{ID: "project"}}}, nil
		case "create":
			*creates++
			return Response{RemoteID: fmt.Sprintf("remote-%d", *creates)}, nil
		default:
			return Response{}, fmt.Errorf("unexpected operation %s", r.Operation)
		}
	}
}

func TestRounding(t *testing.T) {
	for _, tc := range []struct {
		mode          string
		seconds, want int64
	}{{"off", 961, 961}, {"up", 1, 900}, {"up", 900, 900}, {"up", 901, 1800}, {"nearest", 449, 0}, {"nearest", 450, 900}, {"nearest", 1350, 1800}} {
		got, err := (Rounding{Mode: tc.mode, Increment: "15m"}).Seconds(tc.seconds)
		if err != nil || got != tc.want {
			t.Fatalf("%+v got %d %v", tc, got, err)
		}
	}
	for _, r := range []Rounding{{Mode: "bogus"}, {Mode: "up", Increment: "0s"}, {Mode: "up", Increment: "1.5s"}, {Mode: "up", Increment: "25h"}} {
		if _, err := r.Seconds(10); err == nil {
			t.Fatalf("accepted %+v", r)
		}
	}
}

func TestSyncRoundsEachEntryAndSkipsSecondRun(t *testing.T) {
	c, frames := fixture()
	second := frames[0]
	second.ID = "frame-2"
	frames = append(frames, second)
	dir := t.TempDir()
	creates := 0
	var out bytes.Buffer
	for i := 0; i < 2; i++ {
		if err := Sync(context.Background(), dir, c, frames, Options{Connection: "work"}, mockCall(&creates), &out); err != nil {
			t.Fatal(err)
		}
	}
	if creates != 2 {
		t.Fatalf("duplicate uploads: %d", creates)
	}
	l, err := LoadLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range l.Records {
		if r.RecordedSeconds != 960 || r.ExportSeconds != 1800 || r.RemoteID == "" || r.Status != "synced" {
			t.Fatalf("bad receipt %+v", r)
		}
	}
	if *frames[0].Stop != 1960 {
		t.Fatal("local frame was changed")
	}
	if l.Records["frame-1"].Entry.Description != "PORTAL-42" {
		t.Fatal("ticket not preserved")
	}
}

func TestTimetableCoexistsWithClockifyAndRetriesPending(t *testing.T) {
	c, frames := fixture()
	c.Connections["timetable"] = Connection{Plugin: "timetable", BaseURL: "https://timetable.example", UserID: "user", APIKeyEnv: "TIMETABLE_TOKEN", Rounding: Rounding{Mode: "off"}}
	c.Routes = map[string]map[string]Mapping{"timetable": {"portal": {Connection: "timetable", ProjectID: "other-project"}}}
	dir := t.TempDir()
	creates := 0
	if err := Sync(context.Background(), dir, c, frames, Options{Connection: "work"}, mockCall(&creates), io.Discard); err != nil {
		t.Fatal(err)
	}
	timetableCreates := 0
	call := func(_ context.Context, r Request) (Response, error) {
		switch r.Operation {
		case "check":
			return Response{UserID: "user"}, nil
		case "projects":
			return Response{Projects: []Project{{ID: "other-project"}}}, nil
		case "create":
			timetableCreates++
			if r.FrameID != "frame-1" {
				t.Fatalf("missing stable ID: %+v", r)
			}
			if timetableCreates == 1 {
				return Response{}, errors.New("response lost")
			}
			return Response{RemoteID: "remote-timetable"}, nil
		}
		return Response{}, fmt.Errorf("unexpected operation %s", r.Operation)
	}
	if err := Sync(context.Background(), dir, c, frames, Options{Connection: "timetable"}, call, io.Discard); err == nil {
		t.Fatal("expected uncertain result")
	}
	if err := Sync(context.Background(), dir, c, frames, Options{Connection: "timetable"}, call, io.Discard); err != nil {
		t.Fatal(err)
	}
	ledger, err := LoadLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Records) != 2 || ledger.Records["frame-1"].Status != "synced" || ledger.Records["timetable:frame-1"].Status != "synced" || timetableCreates != 2 {
		t.Fatalf("wrong receipts or retry count: %+v, %d", ledger.Records, timetableCreates)
	}
}

func TestPendingSavedBeforeRequestAndNeverBlindlyRetried(t *testing.T) {
	c, frames := fixture()
	dir := t.TempDir()
	creates := 0
	base := mockCall(&creates)
	call := func(ctx context.Context, r Request) (Response, error) {
		if r.Operation == "create" {
			creates++
			ledger, err := LoadLedger(dir)
			if err != nil || ledger.Records[frames[0].ID].Status != "pending" {
				t.Fatal("create happened before durable intent")
			}
			return Response{}, errors.New("response lost")
		}
		return base(ctx, r)
	}
	for i := 0; i < 2; i++ {
		if err := Sync(context.Background(), dir, c, frames, Options{Connection: "work"}, call, io.Discard); err == nil {
			t.Fatal("expected uncertain upload error")
		}
	}
	if creates != 1 {
		t.Fatal("retried uncertain create")
	}
	l, _ := LoadLedger(dir)
	pending := l.Records["frame-1"]
	resolver := func(_ context.Context, r Request) (Response, error) {
		return Response{RemoteID: r.RemoteID, UserID: "user", Entry: &pending.Entry}, nil
	}
	if err := Resolve(context.Background(), dir, "work", "frame-1", "remote-1", c.Connections["work"], resolver); err != nil {
		t.Fatal(err)
	}
	if err := Sync(context.Background(), dir, c, frames, Options{Connection: "work"}, call, io.Discard); err != nil {
		t.Fatal(err)
	}
	if creates != 1 {
		t.Fatal("resolved receipt uploaded again")
	}
}

func TestPartialFailurePreservesSuccess(t *testing.T) {
	c, frames := fixture()
	second := frames[0]
	second.ID = "frame-2"
	frames = append(frames, second)
	dir := t.TempDir()
	creates := 0
	base := mockCall(&creates)
	call := func(ctx context.Context, r Request) (Response, error) {
		if r.Operation == "create" && creates == 1 {
			creates++
			return Response{}, errors.New("failed")
		}
		return base(ctx, r)
	}
	if err := Sync(context.Background(), dir, c, frames, Options{Connection: "work"}, call, io.Discard); err == nil {
		t.Fatal("expected failure")
	}
	l, _ := LoadLedger(dir)
	if l.Records["frame-1"].Status != "synced" || l.Records["frame-2"].Status != "pending" {
		t.Fatalf("lost partial progress %+v", l)
	}
	if err := Sync(context.Background(), dir, c, frames, Options{Connection: "work"}, call, io.Discard); err == nil {
		t.Fatal("expected pending block")
	}
	if creates != 2 {
		t.Fatal("partial retry created duplicates")
	}
}

func TestDryRunAndCanceledReviewMakeNoRequests(t *testing.T) {
	c, frames := fixture()
	for _, opts := range []Options{{Connection: "work", DryRun: true}, {Connection: "work", Review: func([]Receipt) ([]Receipt, error) { return nil, errors.New("canceled") }}} {
		dir := t.TempDir()
		called := false
		_ = Sync(context.Background(), dir, c, frames, opts, func(context.Context, Request) (Response, error) { called = true; return Response{}, nil }, io.Discard)
		if called {
			t.Fatal("network before approval")
		}
		if _, err := os.Stat(filepath.Join(dir, "integration-sync.json")); !os.IsNotExist(err) {
			t.Fatal("wrote receipt before approval")
		}
	}
}

func TestEditedExportKeepsSourceFingerprint(t *testing.T) {
	c, frames := fixture()
	dir := t.TempDir()
	creates := 0
	opts := Options{Connection: "work", Review: func(queue []Receipt) ([]Receipt, error) {
		queue[0].Entry.Description = "edited"
		queue[0].Entry.End = time.Unix(4600, 0).UTC().Format(time.RFC3339)
		queue[0].ExportSeconds = 3600
		return queue, nil
	}}
	if err := Sync(context.Background(), dir, c, frames, opts, mockCall(&creates), io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := Sync(context.Background(), dir, c, frames, Options{Connection: "work"}, mockCall(&creates), io.Discard); err != nil {
		t.Fatal(err)
	}
	if creates != 1 {
		t.Fatal("manual export edit caused re-upload")
	}
	l, _ := LoadLedger(dir)
	if l.Records["frame-1"].Entry.Description != "edited" {
		t.Fatal("edit not retained")
	}
}

func TestChangedSourceAndDestinationAreBlocked(t *testing.T) {
	for _, change := range []string{"duration", "target", "mapping", "rounding"} {
		t.Run(change, func(t *testing.T) {
			c, frames := fixture()
			dir := t.TempDir()
			creates := 0
			if err := Sync(context.Background(), dir, c, frames, Options{Connection: "work"}, mockCall(&creates), io.Discard); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "duration":
				*frames[0].Stop++
			case "target":
				v := c.Connections["work"]
				v.WorkspaceID = "other"
				c.Connections["work"] = v
			case "mapping":
				v := c.Projects["portal"]
				v.ProjectID = "other"
				c.Projects["portal"] = v
			case "rounding":
				v := c.Connections["work"]
				v.Rounding.Mode = "off"
				c.Connections["work"] = v
			}
			if err := Sync(context.Background(), dir, c, frames, Options{Connection: "work"}, mockCall(&creates), io.Discard); err == nil {
				t.Fatal("expected changed export block")
			}
			if creates != 1 {
				t.Fatal("reuploaded changed entry")
			}
		})
	}
}

func TestSelectionAndUnmappedProjects(t *testing.T) {
	c, frames := fixture()
	other := frames[0]
	other.ID = "unmapped"
	other.Project = "unmapped"
	frames = append(frames, other)
	dir := t.TempDir()
	creates := 0
	if err := Sync(context.Background(), dir, c, frames, Options{Connection: "work"}, mockCall(&creates), io.Discard); err == nil {
		t.Fatal("unmapped project silently ignored")
	}
	if creates != 0 {
		t.Fatal("uploaded partial batch before resolving mappings")
	}
	opts := Options{Connection: "work", From: time.Unix(1000, 0), To: time.Unix(1001, 0), SkipProjects: map[string]bool{"unmapped": true}}
	if err := Sync(context.Background(), dir, c, frames, opts, mockCall(&creates), io.Discard); err != nil {
		t.Fatal(err)
	}
	if creates != 1 {
		t.Fatal("explicit project skip failed")
	}
}

func TestLedgerCorruptionAndLockFailClosed(t *testing.T) {
	dir := t.TempDir()
	for _, data := range []string{"", "null", "{}", "{broken", `{"version":2,"records":{}}`, `{"version":1,"records":{"x":{"frame_id":"x","status":"synced"}}}`} {
		if err := os.WriteFile(filepath.Join(dir, "integration-sync.json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadLedger(dir); err == nil {
			t.Fatalf("accepted corrupt ledger %s", data)
		}
	}
	unlock, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(dir); err == nil {
		t.Fatal("concurrent lock accepted")
	}
	unlock()
	unlock, err = Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}

func TestRejectDuplicateAndMissingFrameIDs(t *testing.T) {
	for _, kind := range []string{"duplicate", "missing"} {
		c, frames := fixture()
		if kind == "duplicate" {
			frames = append(frames, frames[0])
		} else {
			frames[0].ID = ""
		}
		creates := 0
		if err := Sync(context.Background(), t.TempDir(), c, frames, Options{Connection: "work"}, mockCall(&creates), io.Discard); err == nil {
			t.Fatal("accepted invalid identity")
		}
		if creates != 0 {
			t.Fatal("unsafe upload")
		}
	}
}

func TestExplicitRetryOnlyResetsPending(t *testing.T) {
	c, frames := fixture()
	dir := t.TempDir()
	r, err := Projection(frames[0], "work", c.Connections["work"], c.Projects["portal"])
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveLedger(dir, Ledger{Version: 1, Records: map[string]Receipt{r.FrameID: r}}); err != nil {
		t.Fatal(err)
	}
	if err := RetryPending(dir, "wrong", r.FrameID, c.Connections["work"]); err == nil {
		t.Fatal("reset other connection")
	}
	if err := RetryPending(dir, "work", r.FrameID, c.Connections["work"]); err != nil {
		t.Fatal(err)
	}
	creates := 0
	if err := Sync(context.Background(), dir, c, frames, Options{Connection: "work"}, mockCall(&creates), io.Discard); err != nil {
		t.Fatal(err)
	}
	if creates != 1 {
		t.Fatal("explicit retry not uploaded")
	}
	if err := RetryPending(dir, "work", r.FrameID, c.Connections["work"]); err == nil {
		t.Fatal("reset successful receipt")
	}
}

func TestResolveRejectsDifferentRemoteEntry(t *testing.T) {
	c, frames := fixture()
	dir := t.TempDir()
	r, _ := Projection(frames[0], "work", c.Connections["work"], c.Projects["portal"])
	if err := SaveLedger(dir, Ledger{Version: 1, Records: map[string]Receipt{r.FrameID: r}}); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"user", "entry"} {
		response := Response{RemoteID: "remote", UserID: "user", Entry: &r.Entry}
		entry := r.Entry
		if change == "user" {
			response.UserID = "other"
		} else {
			entry.Description = "different"
			response.Entry = &entry
		}
		call := func(context.Context, Request) (Response, error) { return response, nil }
		if err := Resolve(context.Background(), dir, "work", r.FrameID, "remote", c.Connections["work"], call); err == nil {
			t.Fatal("accepted mismatched remote")
		}
	}
}

func TestCleanDescriptionsAndLegacyReceipts(t *testing.T) {
	c, frames := fixture()
	connection := c.Connections["work"]
	mapping := c.Projects["portal"]
	r, err := Projection(frames[0], "work", connection, mapping)
	if err != nil {
		t.Fatal(err)
	}
	if r.Entry.Description != "PORTAL-42" {
		t.Fatalf("unexpected description %q", r.Entry.Description)
	}
	for _, edited := range []bool{false, true} {
		dir := t.TempDir()
		old := r
		old.Entry.Description = "portal +PORTAL-42"
		if !edited {
			old.Entry.Description += " [burrowtime:" + old.FrameID + "]"
		}
		old.Fingerprint = exportFingerprint(old.Entry, old.RecordedSeconds, connection.Rounding)
		if edited {
			old.Entry.Description = "custom reviewed text [burrowtime:" + old.FrameID + "]"
		}
		old.Status = "synced"
		old.RemoteID = "already-created"
		if err := SaveLedger(dir, Ledger{Version: 1, Records: map[string]Receipt{old.FrameID: old}}); err != nil {
			t.Fatal(err)
		}
		creates := 0
		if err := Sync(context.Background(), dir, c, frames, Options{Connection: "work"}, mockCall(&creates), io.Discard); err != nil {
			t.Fatal(err)
		}
		if creates != 0 {
			t.Fatal("legacy entry uploaded again")
		}
		changed := append([]store.Frame(nil), frames...)
		changed[0].Tags = []string{"PORTAL-99"}
		if err := Sync(context.Background(), dir, c, changed, Options{Connection: "work"}, mockCall(&creates), io.Discard); err == nil {
			t.Fatal("real edit was hidden by migration")
		}
	}
}

func TestTagOnlyDescriptions(t *testing.T) {
	c, frames := fixture()
	f := frames[0]
	f.Project = "sema"
	for _, tc := range []struct {
		tags []string
		want string
	}{{[]string{"SEMA-123"}, "SEMA-123"}, {[]string{"review", "SEMA-123"}, "SEMA-123 review"}, {nil, ""}, {[]string{"+SEMA-123"}, "SEMA-123"}} {
		f.Tags = tc.tags
		r, err := Projection(f, "work", c.Connections["work"], c.Projects["portal"])
		if err != nil || r.Entry.Description != tc.want {
			t.Fatalf("%v: %q %v", tc.tags, r.Entry.Description, err)
		}
	}
}
