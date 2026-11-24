package timeutil

import (
	"testing"
	"time"
)

func loc(t *testing.T) *time.Location {
	t.Helper()
	l, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestParseRFC3339PreservesOffset(t *testing.T) {
	got, err := Parse("2026-11-26T10:00:00+05:30", loc(t), time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hour() != 10 || got.Minute() != 0 {
		t.Fatalf("unexpected time: %v", got)
	}
	_, offset := got.Zone()
	if offset != 5*3600+30*60 {
		t.Fatalf("offset = %d", offset)
	}
}

func TestParseDateOnly(t *testing.T) {
	l := loc(t)
	start, err := Parse("2026-11-26", l, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	if start.Hour() != 0 {
		t.Fatalf("expected midnight, got %v", start)
	}
	end, err := Parse("2026-11-26", l, time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	if end.Hour() != 23 || end.Minute() != 59 {
		t.Fatalf("expected end of day, got %v", end)
	}
}

func TestParseRelative(t *testing.T) {
	l := loc(t)
	now := time.Date(2026, 11, 25, 14, 30, 0, 0, l)
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"today", "2026-11-25"},
		{"tomorrow", "2026-11-26"},
		{"yesterday", "2026-11-24"},
		{"+3d", "2026-11-28"},
		{"-2d", "2026-11-23"},
		{"monday", "2026-11-30"},
	} {
		got, err := Parse(tc.in, l, now, false)
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if got.Format("2006-01-02") != tc.want {
			t.Errorf("%s: got %s, want %s", tc.in, got.Format("2006-01-02"), tc.want)
		}
	}
}

func TestWeekMonthRanges(t *testing.T) {
	l := loc(t)
	now := time.Date(2026, 11, 25, 14, 0, 0, 0, l)
	ws, we := WeekRange(now, l)
	if ws.Weekday() != time.Monday || we.Weekday() != time.Sunday {
		t.Fatalf("bad week range: %v - %v", ws, we)
	}
	ms, me := MonthRange(now, l)
	if ms.Day() != 1 || me.Day() != 30 {
		t.Fatalf("bad month range: %v - %v", ms, me)
	}
}

func TestDayStartAcrossDST(t *testing.T) {
	l, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip(err)
	}
	// Spring forward 2026-03-08. Wall-clock boundaries must stay at midnight
	// and 23:59:59 in the local zone regardless of the offset shift.
	d := time.Date(2026, 3, 8, 23, 0, 0, 0, l)
	start := DayStart(d, l)
	end := DayEnd(d, l)
	if start.Hour() != 0 || start.Day() != 8 {
		t.Fatalf("wrong start: %v", start)
	}
	if end.Hour() != 23 || end.Minute() != 59 || end.Day() != 8 {
		t.Fatalf("wrong end: %v", end)
	}
	if _, startOffset := start.Zone(); startOffset != -5*3600 {
		t.Fatalf("start offset = %d", startOffset)
	}
	if _, endOffset := end.Zone(); endOffset != -4*3600 {
		t.Fatalf("end offset = %d", endOffset)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0 minutes"},
		{30 * time.Minute, "30 minutes"},
		{time.Minute, "1 minute"},
		{time.Hour, "1 hour"},
		{90 * time.Minute, "1 hour 30 minutes"},
	}
	for _, tc := range cases {
		if got := FormatDuration(tc.in); got != tc.want {
			t.Errorf("%v: got %q, want %q", tc.in, got, tc.want)
		}
	}
}
