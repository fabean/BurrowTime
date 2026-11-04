package report

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fabean/BurrowTime/internal/store"
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

func TestReportJSONAndDayBounds(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		seconds Seconds
		want    string
	}{{"whole", 12, "12.0"}, {"fraction", 1.25, "1.25"}, {"negative", -0.5, "-0.5"}} {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.seconds)
			if err != nil || string(data) != tt.want || FormatSeconds(tt.seconds) != tt.want {
				t.Fatalf("JSON=%s err=%v", data, err)
			}
		})
	}
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	midday := time.Date(2026, 3, 8, 12, 30, 0, 0, loc)
	from, to := DayBounds(midday, midday)
	if from.Hour() != 0 || to.Hour() != 23 || to.Minute() != 59 || to.Second() != 59 || to.Nanosecond() != 999999000 || to.Sub(from) != 23*time.Hour-time.Microsecond {
		t.Fatalf("DST day bounds=%v to %v duration=%v", from, to, to.Sub(from))
	}
	data, err := json.Marshal(Timespan{From: from, To: to})
	if err != nil || !strings.Contains(string(data), `"from":"2026-03-08T00:00:00-05:00"`) || !strings.Contains(string(data), `"to":"2026-03-08T23:59:59.999999-04:00"`) {
		t.Fatalf("timespan=%s err=%v", data, err)
	}
}

func TestFilterProjectTagExclusionsAndPartialStop(t *testing.T) {
	t.Parallel()
	frames := []store.Frame{
		{Start: 1, Stop: nil, Project: "running"},
		{Start: 1, Stop: ptr(5), Project: "other", Tags: []string{"keep"}},
		{Start: 1, Stop: ptr(5), Project: "ignored", Tags: []string{"keep"}},
		{Start: 1, Stop: ptr(5), Project: "p", Tags: []string{"ignored"}},
		{Start: 1, Stop: ptr(20), Project: "p", Tags: []string{"keep"}},
	}
	opts := Options{From: time.Unix(0, 0), To: time.Unix(10, 500000000), Projects: []string{"p", "ignored"}, IgnoreProjects: []string{"ignored"}, IgnoreTags: []string{"ignored"}, IncludePartial: true}
	got := FilterActive(frames, []store.ActiveTimer{{ID: "inactive"}}, time.Unix(9, 0), opts)
	if len(got) != 1 || *got[0].Stop != 10 || got[0].StopMicros != 500000 || *frames[4].Stop != 20 {
		t.Fatalf("filtered=%#v original=%#v", got, frames)
	}
	if summary := Build(got, opts); summary.Time != 9.5 {
		t.Fatalf("summary=%#v", summary)
	}
	opts.IncludeCurrent = true
	if got := FilterActive(nil, []store.ActiveTimer{{}}, time.Unix(9, 0), opts); len(got) != 0 {
		t.Fatalf("inactive timer projected: %#v", got)
	}
}
