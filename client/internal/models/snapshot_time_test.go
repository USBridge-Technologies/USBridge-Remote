package models

import (
	"testing"
	"time"
)

func TestParseSnapshotTimePrefersUnixTimestamp(t *testing.T) {
	unix := time.Date(2026, 9, 11, 13, 51, 0, 0, time.UTC).Unix()
	// Naive date string two hours ahead (what UTC+2 wall clock looks like
	// if someone stuffed local time into `date` without a zone). Must not
	// win over the unix timestamp.
	got := parseSnapshotTime(unix, "2026-09-11 15:51:00")
	want := time.Unix(unix, 0).In(time.Local)
	if !got.Equal(want) {
		t.Fatalf("parseSnapshotTime(unix, naive date) = %v, want %v (unix must win so the row is not shifted by the client timezone)", got, want)
	}
}

func TestParseSnapshotTimeNaiveDateUsesLocal(t *testing.T) {
	got := parseSnapshotTime(0, "2026-09-11 15:51:00")
	want, err := time.ParseInLocation("2006-01-02 15:04:05", "2026-09-11 15:51:00", time.Local)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(want) {
		t.Fatalf("parseSnapshotTime(0, naive date) = %v, want %v", got, want)
	}
}

func TestParseSnapshotTimeRFC3339UTC(t *testing.T) {
	got := parseSnapshotTime(0, "2026-09-11T13:51:00Z")
	want := time.Date(2026, 9, 11, 13, 51, 0, 0, time.UTC).In(time.Local)
	if !got.Equal(want) {
		t.Fatalf("parseSnapshotTime RFC3339 = %v, want %v", got, want)
	}
}
