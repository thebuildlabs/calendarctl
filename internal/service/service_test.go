package service

import (
	"context"
	"testing"
	"time"

	"github.com/thebuildlabs/calendarctl/internal/calendar"
	"github.com/thebuildlabs/calendarctl/internal/config"
	ce "github.com/thebuildlabs/calendarctl/internal/errors"
	"github.com/thebuildlabs/calendarctl/internal/logging"
	"github.com/thebuildlabs/calendarctl/internal/providers/mock"
)

func testService(t *testing.T) (*Service, *mock.Provider) {
	t.Helper()
	cfg := config.Default()
	cfg.Timezone = "Asia/Kolkata"
	p := mock.New()
	svc, err := NewService(p, cfg, logging.Noop())
	if err != nil {
		t.Fatal(err)
	}
	return svc, p
}

func TestResolveCalendar(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	c, err := svc.ResolveCalendar(ctx, "Work")
	if err != nil || c.ID != "cal-work-0001" {
		t.Fatalf("by title: %+v %v", c, err)
	}
	c, err = svc.ResolveCalendar(ctx, "cal-per")
	if err != nil || c.Title != "Personal" {
		t.Fatalf("by prefix: %+v %v", c, err)
	}
	c, err = svc.ResolveCalendar(ctx, "cal")
	if err == nil {
		t.Fatal("expected ambiguous prefix error")
	}
	var ceErr *ce.Error
	if !asCE(err, &ceErr) || ceErr.Code != ce.CodeAmbiguous {
		t.Fatalf("expected ambiguous code, got %v", err)
	}
	if _, err := svc.ResolveCalendar(ctx, "nope"); err == nil {
		t.Fatal("expected not found")
	}
}

func asCE(err error, target **ce.Error) bool {
	for err != nil {
		if e, ok := err.(*ce.Error); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestBuildQuery(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()

	q, err := svc.BuildQuery(ctx, calendar.QueryOptions{Today: true})
	if err != nil {
		t.Fatal(err)
	}
	if q.From.Day() != svc.Now().Day() {
		t.Fatalf("today range: %v", q.From)
	}
	_, err = svc.BuildQuery(ctx, calendar.QueryOptions{Today: true, Week: true})
	if err == nil {
		t.Fatal("expected mutually exclusive flag error")
	}
	_, err = svc.BuildQuery(ctx, calendar.QueryOptions{From: "2026-11-25"})
	if err == nil {
		t.Fatal("expected --to requirement")
	}
	q, err = svc.BuildQuery(ctx, calendar.QueryOptions{From: "2026-11-25", To: "2026-11-30"})
	if err != nil || !q.To.After(q.From) {
		t.Fatalf("range: %v %v", q, err)
	}
}

func TestDuplicateDetection(t *testing.T) {
	svc, _ := testService(t)
	ctx := context.Background()
	loc := svc.Location()
	in := calendar.EventInput{
		CalendarID: "cal-work-0001",
		Title:      "Team Standup",
		Start:      time.Date(2026, 11, 25, 9, 0, 0, 0, loc),
		End:        time.Date(2026, 11, 25, 10, 0, 0, 0, loc),
	}
	dup, err := svc.FindDuplicate(ctx, in)
	if err != nil || dup == nil {
		t.Fatalf("expected duplicate: %+v %v", dup, err)
	}
	_, err = svc.CreateEvent(ctx, in, false)
	if err == nil {
		t.Fatal("expected duplicate rejection")
	}
	var ceErr *ce.Error
	if !asCE(err, &ceErr) || ceErr.Code != ce.CodeDuplicate {
		t.Fatalf("expected duplicate code, got %v", err)
	}
	created, err := svc.CreateEvent(ctx, in, true)
	if err != nil || created == nil {
		t.Fatalf("force create failed: %v", err)
	}
}

func TestAvailabilityMergesOverlaps(t *testing.T) {
	svc, p := testService(t)
	ctx := context.Background()
	loc := svc.Location()
	day := time.Date(2026, 11, 25, 0, 0, 0, 0, loc)

	// Add an overlapping event to the seeded 09:00-10:00 standup.
	_, err := p.CreateEvent(ctx, calendar.EventInput{
		CalendarID: "cal-work-0001", Title: "Overlap",
		Start: day.Add(9*time.Hour + 30*time.Minute),
		End:   day.Add(10*time.Hour + 30*time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Availability(ctx, AvailabilityOptions{
		From: day, To: day.Add(12 * time.Hour), MeetingMinutes: 30, MaxSuggestions: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The seeded Team Standup (09:00-10:00), recurring Daily Standup
	// (09:00-09:30) and the new overlapping event all merge into one slot.
	if result.EventCount != 4 {
		t.Fatalf("event count = %d", result.EventCount)
	}
	if len(result.Busy) != 2 {
		t.Fatalf("expected 2 merged busy slots, got %d", len(result.Busy))
	}
	if result.Busy[0].Start.Hour() != 9 || result.Busy[0].End.Hour() != 10 || result.Busy[0].End.Minute() != 30 {
		t.Fatalf("merged busy slot = %+v", result.Busy[0])
	}
	if result.BusyMinutes != 150 {
		t.Fatalf("busy minutes = %d", result.BusyMinutes)
	}
	if len(result.Free) != 2 {
		t.Fatalf("free slots = %d", len(result.Free))
	}
	if len(result.Suggestions) == 0 {
		t.Fatal("expected suggestions")
	}
}

func TestAvailabilityAllDay(t *testing.T) {
	svc, p := testService(t)
	ctx := context.Background()
	loc := svc.Location()
	// Use a month with no seeded events.
	now := svc.Now()
	day := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, loc)
	_, err := p.CreateEvent(ctx, calendar.EventInput{
		CalendarID: "cal-personal-0002", Title: "Holiday", AllDay: true,
		Start: day, End: day.AddDate(0, 0, 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Availability(ctx, AvailabilityOptions{From: day, To: day.AddDate(0, 0, 1), MeetingMinutes: 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Busy) != 1 || !result.Busy[0].Start.Equal(day) || !result.Busy[0].End.Equal(day.AddDate(0, 0, 1)) {
		t.Fatalf("all-day busy = %+v", result.Busy)
	}
	if len(result.Free) != 0 {
		t.Fatalf("expected no free slots, got %+v", result.Free)
	}
}
