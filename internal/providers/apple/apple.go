// Package apple implements calendar.Provider on top of the EventKit Swift
// bridge. All EventKit specifics live in native/apple/EventKitBridge.swift.
package apple

import (
	"context"
	"fmt"

	"github.com/thebuildlabs/calendarctl/internal/bridge"
	"github.com/thebuildlabs/calendarctl/internal/calendar"
	ce "github.com/thebuildlabs/calendarctl/internal/errors"
	"github.com/thebuildlabs/calendarctl/internal/logging"
)

type Provider struct {
	client *bridge.Client
	log    *logging.Logger
}

func New(path string, log *logging.Logger) (*Provider, error) {
	if log == nil {
		log = logging.Noop()
	}
	resolved, err := bridge.Resolve(path)
	if err != nil {
		return nil, err
	}
	for _, warning := range resolved.Warnings {
		log.Warn("bridge path override", logging.F("detail", warning))
	}
	// NewResolved keeps the validated file identity so every call can
	// re-check the helper before executing it.
	return &Provider{client: bridge.NewResolved(resolved, log), log: log}, nil
}

func (p *Provider) Name() string { return "apple" }

// BridgePath returns the resolved path to the native helper binary.
func (p *Provider) BridgePath() string { return p.client.Path }

func (p *Provider) Close() error { return nil }

func (p *Provider) CheckPermissions(ctx context.Context) (*calendar.PermissionStatus, error) {
	var w wirePermission
	if err := p.client.Call(ctx, bridge.CmdCheckPermissions, nil, &w); err != nil {
		return nil, err
	}
	return &calendar.PermissionStatus{State: w.State, Message: w.Message}, nil
}

func (p *Provider) RequestPermissions(ctx context.Context) (*calendar.PermissionStatus, error) {
	var w wirePermission
	if err := p.client.Call(ctx, bridge.CmdRequestPermissions, nil, &w); err != nil {
		return nil, err
	}
	return &calendar.PermissionStatus{State: w.State, Message: w.Message}, nil
}

func (p *Provider) ListCalendars(ctx context.Context) ([]calendar.Calendar, error) {
	var items []wireCalendar
	if err := p.client.Call(ctx, bridge.CmdListCalendars, nil, &items); err != nil {
		return nil, err
	}
	out := make([]calendar.Calendar, 0, len(items))
	for _, w := range items {
		out = append(out, toModelCalendar(w))
	}
	p.log.Debug("calendars loaded", logging.F("count", len(out)))
	return out, nil
}

func (p *Provider) GetCalendar(ctx context.Context, id string) (*calendar.Calendar, error) {
	var w wireCalendar
	if err := p.client.Call(ctx, bridge.CmdGetCalendar, map[string]any{"id": id}, &w); err != nil {
		return nil, err
	}
	if w.ID == "" {
		return nil, ce.NotFound("calendar.get", "calendar "+id)
	}
	c := toModelCalendar(w)
	return &c, nil
}

func (p *Provider) CreateCalendar(ctx context.Context, title string) (*calendar.Calendar, error) {
	var w wireCalendar
	if err := p.client.Call(ctx, bridge.CmdCreateCalendar, map[string]any{"title": title}, &w); err != nil {
		return nil, err
	}
	c := toModelCalendar(w)
	return &c, nil
}

func (p *Provider) DeleteCalendar(ctx context.Context, id string) error {
	return p.client.Call(ctx, bridge.CmdDeleteCalendar, map[string]any{"id": id}, nil)
}

func (p *Provider) ListEvents(ctx context.Context, q calendar.EventQuery) ([]calendar.Event, error) {
	params := map[string]any{
		"from": formatTime(q.From),
		"to":   formatTime(q.To),
	}
	if q.CalendarID != "" {
		params["calendarId"] = q.CalendarID
	}
	if q.Search != "" {
		params["search"] = q.Search
	}
	if q.Limit > 0 {
		params["limit"] = q.Limit
	}
	var items []wireEvent
	if err := p.client.Call(ctx, bridge.CmdListEvents, params, &items); err != nil {
		return nil, err
	}
	out := make([]calendar.Event, 0, len(items))
	for _, w := range items {
		out = append(out, toModel(w))
	}
	p.log.Debug("events loaded", logging.F("count", len(out)))
	return out, nil
}

func (p *Provider) GetEvent(ctx context.Context, id string) (*calendar.Event, error) {
	var w wireEvent
	if err := p.client.Call(ctx, bridge.CmdGetEvent, map[string]any{"id": id}, &w); err != nil {
		return nil, err
	}
	if w.ID == "" {
		return nil, ce.NotFound("event.get", "event "+id)
	}
	e := toModel(w)
	return &e, nil
}

func (p *Provider) CreateEvent(ctx context.Context, in calendar.EventInput) (*calendar.Event, error) {
	params := map[string]any{
		"calendarId":  in.CalendarID,
		"title":       in.Title,
		"description": in.Description,
		"location":    in.Location,
		"url":         in.URL,
		"start":       formatTime(in.Start),
		"end":         formatTime(in.End),
		"allDay":      in.AllDay,
		"timeZone":    in.TimeZone,
		"recurrence":  fromRecurrence(in.Recurrence),
		"reminders":   fromReminders(in.Reminders),
	}
	var w wireEvent
	if err := p.client.Call(ctx, bridge.CmdCreateEvent, params, &w); err != nil {
		return nil, err
	}
	e := toModel(w)
	return &e, nil
}

func (p *Provider) UpdateEvent(ctx context.Context, id string, patch calendar.EventPatch) (*calendar.Event, error) {
	params := map[string]any{"id": id}
	if patch.Title != nil {
		params["title"] = *patch.Title
	}
	if patch.Description != nil {
		params["description"] = *patch.Description
	}
	if patch.Location != nil {
		params["location"] = *patch.Location
	}
	if patch.URL != nil {
		params["url"] = *patch.URL
	}
	if patch.CalendarID != nil {
		params["calendarId"] = *patch.CalendarID
	}
	if patch.Start != nil {
		params["start"] = formatTime(*patch.Start)
	}
	if patch.End != nil {
		params["end"] = formatTime(*patch.End)
	}
	if patch.AllDay != nil {
		params["allDay"] = *patch.AllDay
	}
	if patch.TimeZone != nil {
		params["timeZone"] = *patch.TimeZone
	}
	if patch.Recurrence != nil {
		params["recurrence"] = fromRecurrence(patch.Recurrence)
	}
	if patch.ClearRecurrence {
		params["clearRecurrence"] = true
	}
	if patch.Reminders != nil {
		params["reminders"] = fromReminders(*patch.Reminders)
	}
	if patch.Span != "" {
		params["span"] = patch.Span
	}
	var w wireEvent
	if err := p.client.Call(ctx, bridge.CmdUpdateEvent, params, &w); err != nil {
		return nil, err
	}
	e := toModel(w)
	return &e, nil
}

func (p *Provider) DeleteEvent(ctx context.Context, id string, span string) error {
	params := map[string]any{"id": id}
	if span != "" {
		params["span"] = span
	}
	return p.client.Call(ctx, bridge.CmdDeleteEvent, params, nil)
}

// Assert interface conformance at compile time.
var _ calendar.Provider = (*Provider)(nil)

func (p *Provider) String() string { return fmt.Sprintf("apple(bridge=%s)", p.client.Path) }
