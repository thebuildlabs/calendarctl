package mock

import (
	"context"
	"testing"
	"time"

	"github.com/thebuildlabs/calendarctl/internal/calendar"
	ce "github.com/thebuildlabs/calendarctl/internal/errors"
)

func TestCRUD(t *testing.T) {
	p := New()
	ctx := context.Background()

	cals, err := p.ListCalendars(ctx)
	if err != nil || len(cals) != 4 {
		t.Fatalf("list calendars: %d %v", len(cals), err)
	}
	cal, err := p.CreateCalendar(ctx, "New Calendar")
	if err != nil || cal.Title != "New Calendar" {
		t.Fatalf("create calendar: %+v %v", cal, err)
	}

	in := calendar.EventInput{
		CalendarID: cal.ID, Title: "Test Event",
		Start:     time.Date(2027, 1, 2, 10, 0, 0, 0, time.UTC),
		End:       time.Date(2027, 1, 2, 11, 0, 0, 0, time.UTC),
		Location:  "Room 1",
		Reminders: []calendar.Reminder{{MinutesBefore: 15}},
	}
	created, err := p.CreateEvent(ctx, in)
	if err != nil || created.ID == "" {
		t.Fatalf("create event: %+v %v", created, err)
	}
	got, err := p.GetEvent(ctx, created.ID)
	if err != nil || got.Location != "Room 1" {
		t.Fatalf("get event: %+v %v", got, err)
	}

	title := "Renamed"
	reminders := []calendar.Reminder{{MinutesBefore: 30}}
	updated, err := p.UpdateEvent(ctx, created.ID, calendar.EventPatch{Title: &title, Reminders: &reminders})
	if err != nil || updated.Title != "Renamed" || len(updated.Reminders) != 1 {
		t.Fatalf("update event: %+v %v", updated, err)
	}

	found, err := p.ListEvents(ctx, calendar.EventQuery{
		From: in.Start.Add(-time.Hour), To: in.End.Add(time.Hour), Search: "renamed",
	})
	if err != nil || len(found) != 1 {
		t.Fatalf("list events: %d %v", len(found), err)
	}

	if err := p.DeleteEvent(ctx, created.ID, "this"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.GetEvent(ctx, created.ID); err == nil {
		t.Fatal("expected not found after delete")
	}
	if err := p.DeleteCalendar(ctx, cal.ID); err != nil {
		t.Fatal(err)
	}
}

func TestPermissions(t *testing.T) {
	p := New()
	ctx := context.Background()
	p.SetPermission("denied")
	_, err := p.ListCalendars(ctx)
	if err == nil {
		t.Fatal("expected permission error")
	}
	e, ok := err.(*ce.Error)
	if !ok || e.Code != ce.CodePermission {
		t.Fatalf("wrong error: %v", err)
	}
	p.SetPermission("notDetermined")
	status, err := p.RequestPermissions(ctx)
	if err != nil || status.State != "authorized" {
		t.Fatalf("request: %+v %v", status, err)
	}
}

func TestDeniedPermissionBlocksEveryOperation(t *testing.T) {
	p := New()
	ctx := context.Background()
	p.SetPermission("denied")

	checks := map[string]func() error{
		"listCalendars": func() error { _, err := p.ListCalendars(ctx); return err },
		"getCalendar":   func() error { _, err := p.GetCalendar(ctx, "cal-work-0001"); return err },
		"createCalendar": func() error {
			_, err := p.CreateCalendar(ctx, "Nope")
			return err
		},
		"deleteCalendar": func() error { return p.DeleteCalendar(ctx, "cal-work-0001") },
		"listEvents": func() error {
			_, err := p.ListEvents(ctx, calendar.EventQuery{})
			return err
		},
		"getEvent": func() error { _, err := p.GetEvent(ctx, "evt-standup-0001"); return err },
		"createEvent": func() error {
			_, err := p.CreateEvent(ctx, calendar.EventInput{Title: "Nope"})
			return err
		},
		"updateEvent": func() error {
			_, err := p.UpdateEvent(ctx, "evt-standup-0001", calendar.EventPatch{})
			return err
		},
		"deleteEvent": func() error { return p.DeleteEvent(ctx, "evt-standup-0001", "this") },
	}
	for name, check := range checks {
		err := check()
		e, ok := err.(*ce.Error)
		if !ok || e.Code != ce.CodePermission {
			t.Errorf("%s: expected permission error, got %v", name, err)
		}
	}
}

func TestWriteOnlyAllowsMutationsButNotReads(t *testing.T) {
	p := New()
	ctx := context.Background()
	p.SetPermission("writeOnly")

	if _, err := p.ListCalendars(ctx); err == nil {
		t.Fatal("write-only state must not allow reads")
	}
	if _, err := p.CreateCalendar(ctx, "Allowed"); err != nil {
		t.Fatalf("write-only state should allow writes: %v", err)
	}
}

func TestPersistence(t *testing.T) {
	path := t.TempDir() + "/store.json"
	p, err := NewFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.CreateCalendar(context.Background(), "Persisted"); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	p2, err := NewFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cals, err := p2.ListCalendars(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range cals {
		if c.Title == "Persisted" {
			found = true
		}
	}
	if !found {
		t.Fatal("persisted calendar missing after reload")
	}
}
