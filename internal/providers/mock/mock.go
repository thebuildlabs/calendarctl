// Package mock provides an in-memory calendar.Provider used by tests, demos
// and non-macOS platforms. It intentionally mirrors native provider semantics
// (IDs, recurrence, spans, permission states) without touching EventKit.
package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/thebuildlabs/calendarctl/internal/calendar"
	ce "github.com/thebuildlabs/calendarctl/internal/errors"
)

type Provider struct {
	mu          sync.RWMutex
	calendars   map[string]*calendar.Calendar
	events      map[string]*calendar.Event
	permission  calendar.PermissionStatus
	counter     int
	StorePath   string
	ReadOnStart bool
}

func New() *Provider {
	p := &Provider{
		calendars:  map[string]*calendar.Calendar{},
		events:     map[string]*calendar.Event{},
		permission: calendar.PermissionStatus{State: "authorized"},
	}
	p.Seed()
	return p
}

// NewFromFile loads a persisted mock store, or creates a seeded one.
func NewFromFile(path string) (*Provider, error) {
	p := New()
	p.StorePath = path
	if path == "" {
		return p, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return p, nil
		}
		return nil, err
	}
	var data struct {
		Calendars []calendar.Calendar `json:"calendars"`
		Events    []calendar.Event    `json:"events"`
		State     string              `json:"permissionState"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, fmt.Errorf("parse mock store %s: %w", path, err)
	}
	p.calendars = map[string]*calendar.Calendar{}
	p.events = map[string]*calendar.Event{}
	for i := range data.Calendars {
		c := data.Calendars[i]
		p.calendars[c.ID] = &c
	}
	for i := range data.Events {
		e := data.Events[i]
		p.events[e.ID] = &e
	}
	if data.State != "" {
		p.permission.State = data.State
	}
	return p, nil
}

// Seed adds a small realistic dataset anchored around the current day.
func (p *Provider) Seed() {
	loc := time.Local
	now := time.Now().In(loc)
	y, m, d := now.Date()
	at := func(dayOffset, hour, minute int) time.Time {
		return time.Date(y, m, d+dayOffset, hour, minute, 0, 0, loc)
	}
	work := &calendar.Calendar{ID: "cal-work-0001", Title: "Work", Source: "iCloud", Type: "icloud", Color: "#1E88E5", Writable: true}
	personal := &calendar.Calendar{ID: "cal-personal-0002", Title: "Personal", Source: "iCloud", Type: "icloud", Color: "#43A047", Writable: true}
	team := &calendar.Calendar{ID: "cal-team-0003", Title: "Team Calendar", Source: "Google", Type: "caldav", Color: "#FB8C00", Writable: true}
	birthdays := &calendar.Calendar{ID: "cal-bday-0004", Title: "Birthdays", Source: "Other", Type: "birthdays", Writable: false, Subscribed: true}
	p.calendars = map[string]*calendar.Calendar{work.ID: work, personal.ID: personal, team.ID: team, birthdays.ID: birthdays}
	p.events = map[string]*calendar.Event{}

	seed := func(id, calID, title string, start, end time.Time, desc, location string) {
		e := &calendar.Event{
			ID: id, CalendarID: calID, CalendarTitle: p.calendars[calID].Title,
			Title: title, Start: start, End: end, Description: desc, Location: location,
			Status: "confirmed", TimeZone: loc.String(),
			Reminders: []calendar.Reminder{{MinutesBefore: 10}},
		}
		p.events[id] = e
	}
	seed("evt-standup-0001", work.ID, "Team Standup", at(0, 9, 0), at(0, 10, 0), "Daily sync", "Zoom")
	seed("evt-review-0002", work.ID, "Project Review", at(0, 11, 0), at(0, 12, 0), "Sprint review", "Room A")
	seed("evt-study-0003", personal.ID, "Study Session", at(0, 14, 0), at(0, 15, 0), "", "Home")
	seed("evt-client-0004", work.ID, "Client Meeting", at(0, 16, 0), at(0, 17, 0), "Quarterly check-in", "Office")
	seed("evt-standup-0005", work.ID, "Team Standup", at(1, 9, 0), at(1, 10, 0), "Daily sync", "Zoom")
	seed("evt-design-0006", team.ID, "Design Sync", at(1, 13, 0), at(1, 14, 0), "", "Meet")
	seed("evt-assignment-0007", personal.ID, "Assignment Due", at(2, 18, 0), at(2, 19, 0), "Submit final report", "")
	seed("evt-focus-0008", work.ID, "Focus Time", at(3, 10, 0), at(3, 12, 0), "No meetings", "")
	seed("evt-weekly-0009", work.ID, "Weekly Planning", at(4, 9, 30), at(4, 10, 30), "", "Room B")
	allDay := &calendar.Event{
		ID: "evt-holiday-0010", CalendarID: personal.ID, CalendarTitle: personal.Title,
		Title: "Holiday", AllDay: true,
		Start:  time.Date(y, m, d+5, 0, 0, 0, 0, loc),
		End:    time.Date(y, m, d+6, 0, 0, 0, 0, loc),
		Status: "confirmed",
	}
	p.events[allDay.ID] = allDay

	rec := &calendar.Recurrence{Frequency: "daily", Interval: 1}
	until := at(30, 23, 59)
	rec.Until = &until
	standup := &calendar.Event{
		ID: "evt-standup-rec-0011", CalendarID: work.ID, CalendarTitle: work.Title,
		Title: "Daily Standup", Start: at(0, 9, 0), End: at(0, 9, 30),
		Recurrence: rec, Status: "confirmed", TimeZone: loc.String(),
	}
	p.events[standup.ID] = standup
	p.counter = 100
}

func (p *Provider) Name() string { return "mock" }
func (p *Provider) Close() error { return p.persist() }

func (p *Provider) persist() error {
	if p.StorePath == "" {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	var data struct {
		Calendars []calendar.Calendar `json:"calendars"`
		Events    []calendar.Event    `json:"events"`
		State     string              `json:"permissionState"`
	}
	for _, c := range p.calendars {
		data.Calendars = append(data.Calendars, *c)
	}
	for _, e := range p.events {
		data.Events = append(data.Events, *e)
	}
	data.State = p.permission.State
	sort.Slice(data.Calendars, func(i, j int) bool { return data.Calendars[i].ID < data.Calendars[j].ID })
	sort.Slice(data.Events, func(i, j int) bool { return data.Events[i].ID < data.Events[j].ID })
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.StorePath, b, 0o600)
}

func (p *Provider) SetPermission(state string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.permission.State = state
}

func (p *Provider) CheckPermissions(context.Context) (*calendar.PermissionStatus, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s := p.permission
	return &s, nil
}

func (p *Provider) RequestPermissions(ctx context.Context) (*calendar.PermissionStatus, error) {
	p.mu.Lock()
	if p.permission.State == "notDetermined" {
		p.permission.State = "authorized"
	}
	p.mu.Unlock()
	return p.CheckPermissions(ctx)
}

// requireAccess mirrors the native helper's permission gate. The caller must
// hold p.mu. Reads need full access; mutations also accept write-only access.
func (p *Provider) requireAccess(op string, writeOnlyAllowed bool) error {
	switch p.permission.State {
	case "authorized":
		return nil
	case "writeOnly":
		if writeOnlyAllowed {
			return nil
		}
	}
	return ce.Permission(op,
		"calendar access is denied or unavailable",
		"Grant access in System Settings → Privacy & Security → Calendars")
}

func (p *Provider) ListCalendars(context.Context) ([]calendar.Calendar, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if err := p.requireAccess("calendar.list", false); err != nil {
		return nil, err
	}
	out := make([]calendar.Calendar, 0, len(p.calendars))
	for _, c := range p.calendars {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title) })
	return out, nil
}

func (p *Provider) GetCalendar(_ context.Context, id string) (*calendar.Calendar, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if err := p.requireAccess("calendar.get", false); err != nil {
		return nil, err
	}
	for _, c := range p.calendars {
		if c.ID == id {
			cp := *c
			return &cp, nil
		}
	}
	return nil, ce.NotFound("calendar.get", "calendar "+id)
}

func (p *Provider) CreateCalendar(_ context.Context, title string) (*calendar.Calendar, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.requireAccess("calendar.create", true); err != nil {
		return nil, err
	}
	p.counter++
	c := &calendar.Calendar{
		ID:       fmt.Sprintf("cal-%04d", p.counter),
		Title:    strings.TrimSpace(title),
		Source:   "Local",
		Type:     "local",
		Writable: true,
	}
	p.calendars[c.ID] = c
	_ = p.persistLocked()
	cp := *c
	return &cp, nil
}

func (p *Provider) DeleteCalendar(_ context.Context, id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.requireAccess("calendar.delete", true); err != nil {
		return err
	}
	if _, ok := p.calendars[id]; !ok {
		return ce.NotFound("calendar.delete", "calendar "+id)
	}
	delete(p.calendars, id)
	for eid, e := range p.events {
		if e.CalendarID == id {
			delete(p.events, eid)
		}
	}
	return p.persistLocked()
}

func (p *Provider) ListEvents(_ context.Context, q calendar.EventQuery) ([]calendar.Event, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if err := p.requireAccess("event.list", false); err != nil {
		return nil, err
	}
	var out []calendar.Event
	for _, e := range p.events {
		if q.CalendarID != "" && e.CalendarID != q.CalendarID {
			continue
		}
		if !q.From.IsZero() && !q.To.IsZero() && !timeutilOverlaps(e, q.From, q.To) {
			continue
		}
		if !e.Match(q.Search) {
			continue
		}
		cp := *e
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].AllDay != out[j].AllDay {
			return out[i].AllDay
		}
		if out[i].Start.Equal(out[j].Start) {
			return out[i].Title < out[j].Title
		}
		return out[i].Start.Before(out[j].Start)
	})
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func timeutilOverlaps(e *calendar.Event, from, to time.Time) bool {
	start, end := e.Start, e.End
	if e.AllDay {
		start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
		end = time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 0, end.Location())
	}
	return start.Before(to) && from.Before(end)
}

func (p *Provider) GetEvent(_ context.Context, id string) (*calendar.Event, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if err := p.requireAccess("event.get", false); err != nil {
		return nil, err
	}
	for _, e := range p.events {
		if e.ID == id {
			cp := *e
			return &cp, nil
		}
	}
	return nil, ce.NotFound("event.get", "event "+id)
}

func (p *Provider) CreateEvent(_ context.Context, in calendar.EventInput) (*calendar.Event, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.requireAccess("event.create", true); err != nil {
		return nil, err
	}
	if _, ok := p.calendars[in.CalendarID]; !ok && in.CalendarID != "" {
		return nil, ce.NotFound("event.create", "calendar "+in.CalendarID)
	}
	p.counter++
	calTitle := ""
	if c, ok := p.calendars[in.CalendarID]; ok {
		calTitle = c.Title
	}
	e := &calendar.Event{
		ID:            fmt.Sprintf("evt-%05d", p.counter),
		CalendarID:    in.CalendarID,
		CalendarTitle: calTitle,
		Title:         in.Title,
		Description:   in.Description,
		Location:      in.Location,
		URL:           in.URL,
		Start:         in.Start,
		End:           in.End,
		AllDay:        in.AllDay,
		TimeZone:      in.TimeZone,
		Recurrence:    in.Recurrence,
		Reminders:     in.Reminders,
		Status:        "confirmed",
		CreatedAt:     nowPtr(),
		ModifiedAt:    nowPtr(),
	}
	p.events[e.ID] = e
	_ = p.persistLocked()
	cp := *e
	return &cp, nil
}

func nowPtr() *time.Time {
	t := time.Now()
	return &t
}

func (p *Provider) UpdateEvent(_ context.Context, id string, patch calendar.EventPatch) (*calendar.Event, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.requireAccess("event.update", true); err != nil {
		return nil, err
	}
	e, ok := p.events[id]
	if !ok {
		return nil, ce.NotFound("event.update", "event "+id)
	}
	if patch.Title != nil {
		e.Title = *patch.Title
	}
	if patch.Description != nil {
		e.Description = *patch.Description
	}
	if patch.Location != nil {
		e.Location = *patch.Location
	}
	if patch.URL != nil {
		e.URL = *patch.URL
	}
	if patch.CalendarID != nil {
		if _, ok := p.calendars[*patch.CalendarID]; !ok {
			return nil, ce.NotFound("event.update", "calendar "+*patch.CalendarID)
		}
		e.CalendarID = *patch.CalendarID
		e.CalendarTitle = p.calendars[*patch.CalendarID].Title
	}
	if patch.Start != nil {
		e.Start = *patch.Start
	}
	if patch.End != nil {
		e.End = *patch.End
	}
	if patch.AllDay != nil {
		e.AllDay = *patch.AllDay
	}
	if patch.TimeZone != nil {
		e.TimeZone = *patch.TimeZone
	}
	if patch.ClearRecurrence {
		e.Recurrence = nil
	} else if patch.Recurrence != nil {
		e.Recurrence = patch.Recurrence
	}
	if patch.Reminders != nil {
		e.Reminders = *patch.Reminders
	}
	e.ModifiedAt = nowPtr()
	_ = p.persistLocked()
	cp := *e
	return &cp, nil
}

func (p *Provider) DeleteEvent(_ context.Context, id, span string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.requireAccess("event.delete", true); err != nil {
		return err
	}
	if _, ok := p.events[id]; !ok {
		return ce.NotFound("event.delete", "event "+id)
	}
	delete(p.events, id)
	return p.persistLocked()
}

func (p *Provider) persistLocked() error {
	// persist() takes RLock, so callers holding the write lock use this path.
	if p.StorePath == "" {
		return nil
	}
	var data struct {
		Calendars []calendar.Calendar `json:"calendars"`
		Events    []calendar.Event    `json:"events"`
		State     string              `json:"permissionState"`
	}
	for _, c := range p.calendars {
		data.Calendars = append(data.Calendars, *c)
	}
	for _, e := range p.events {
		data.Events = append(data.Events, *e)
	}
	data.State = p.permission.State
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.StorePath, b, 0o600)
}

var _ calendar.Provider = (*Provider)(nil)
