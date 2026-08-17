package report

import (
	"testing"
	"time"

	"github.com/josh/burrowtime/internal/store"
)

func TestPartialFilteringAndAnyTagSemantics(t *testing.T) {
	loc := time.UTC
	from := time.Date(2026, 8, 17, 0, 0, 0, 0, loc)
	to := from.Add(24*time.Hour - time.Microsecond)
	a, b := from.Add(-time.Hour).Unix(), from.Add(time.Hour).Unix()
	_ = a
	frames := []store.Frame{{Start: from.Add(-time.Hour).Unix(), Stop: &b, Project: "p", Tags: []string{"one"}}, {Start: from.Add(2 * time.Hour).Unix(), Stop: ptr(from.Add(3 * time.Hour).Unix()), Project: "p", Tags: []string{"two"}}}
	o := Options{From: from, To: to, Tags: []string{"one", "missing"}, IncludePartial: true}
	got := Filter(frames, store.State{}, to, o)
	if len(got) != 1 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].Start != from.Unix() {
		t.Fatalf("frame not clipped: %#v", got[0])
	}
	s := Build(got, o)
	if s.Time != 3600 || s.Projects[0].Tags[0].Time != 3600 {
		t.Fatalf("bad totals: %#v", s)
	}
}

func TestCurrentFrameKeepsEmptyTagsAsJSONArray(t *testing.T) {
	now := time.Unix(200, 0).UTC()
	got := Filter(nil, store.State{Project: "p", Start: 100, Tags: []string{}}, now, Options{
		From: time.Unix(0, 0).UTC(), To: now, IncludeCurrent: true,
	})
	if len(got) != 1 || got[0].Tags == nil {
		t.Fatalf("current frame must retain an empty, non-nil tag list: %#v", got)
	}
}

func TestConcurrentCurrentFramesAreProjected(t *testing.T) {
	now := time.Unix(500, 0)
	timers := []store.ActiveTimer{
		{ID: "primary", Project: "one", Start: 100, Primary: true},
		{ID: "abcdef", Project: "two", Start: 200},
	}
	got := FilterActive(nil, timers, now, Options{From: time.Unix(0, 0), To: time.Unix(600, 0), IncludeCurrent: true})
	if len(got) != 2 || got[0].ID != "current" || got[1].ID != "abcdef" {
		t.Fatalf("projected frames: %#v", got)
	}
	if summary := Build(got, Options{}); summary.Time != 700 {
		t.Fatalf("concurrent total=%v, want 700", summary.Time)
	}
}

func ptr(v int64) *int64 { return &v }
