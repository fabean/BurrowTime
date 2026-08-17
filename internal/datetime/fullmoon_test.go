package datetime

import (
	"testing"
	"time"
)

func TestWatsonFullMoonTable(t *testing.T) {
	before := time.Date(2018, 7, 27, 10, 51, 0, 0, time.UTC)
	got, err := LastFullMoon(before)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2018, 6, 28, 4, 55, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("want %v got %v", want, got)
	}
	after := before.AddDate(0, 0, 1)
	got, err = LastFullMoon(after)
	if err != nil {
		t.Fatal(err)
	}
	want = time.Date(2018, 7, 27, 20, 22, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("want %v got %v", want, got)
	}
	if _, err = LastFullMoon(time.Unix(0, 0)); err == nil {
		t.Fatal("expected range error")
	}
}
