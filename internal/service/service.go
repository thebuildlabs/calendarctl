// Package service orchestrates provider calls and holds provider-agnostic
// business rules: ID resolution, default selection, date-range building,
// duplicate detection and availability computation.
package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/thebuildlabs/calendarctl/internal/calendar"
	"github.com/thebuildlabs/calendarctl/internal/config"
	ce "github.com/thebuildlabs/calendarctl/internal/errors"
	"github.com/thebuildlabs/calendarctl/internal/logging"
	"github.com/thebuildlabs/calendarctl/internal/timeutil"
)

type Service struct {
	provider calendar.Provider
	cfg      *config.Config
	log      *logging.Logger
	loc      *time.Location
}

func NewService(p calendar.Provider, cfg *config.Config, log *logging.Logger) (*Service, error) {
	if p == nil {
		return nil, ce.Unavailable("service", "no calendar provider configured")
	}
	if cfg == nil {
		cfg = config.Default()
	}
	if log == nil {
		log = logging.Noop()
	}
	loc, err := cfg.Location()
	if err != nil {
		return nil, ce.InvalidInput("service", fmt.Sprintf("invalid timezone %q: %v", cfg.Timezone, err))
	}
	return &Service{provider: p, cfg: cfg, log: log, loc: loc}, nil
}

func (s *Service) Provider() calendar.Provider { return s.provider }
func (s *Service) Location() *time.Location    { return s.loc }
func (s *Service) Config() *config.Config      { return s.cfg }

// Now returns the current time in the configured location.
func (s *Service) Now() time.Time { return time.Now().In(s.loc) }

// ResolveCalendar accepts an exact ID, a unique ID prefix, or a title.
func (s *Service) ResolveCalendar(ctx context.Context, ref string) (*calendar.Calendar, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, ce.InvalidInput("calendar.resolve", "empty calendar reference")
	}
	items, err := s.provider.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	var prefixMatches []*calendar.Calendar
	var titleMatch *calendar.Calendar
	for i := range items {
		c := &items[i]
		if c.ID == ref {
			return c, nil
		}
		if strings.HasPrefix(strings.ToLower(c.ID), strings.ToLower(ref)) {
			prefixMatches = append(prefixMatches, c)
		}
		if strings.EqualFold(c.Title, ref) {
			titleMatch = c
		}
	}
	if titleMatch != nil && len(prefixMatches) == 0 {
		return titleMatch, nil
	}
	if len(prefixMatches) == 1 {
		return prefixMatches[0], nil
	}
	if len(prefixMatches) > 1 {
		ids := make([]string, 0, len(prefixMatches))
		for _, c := range prefixMatches {
			ids = append(ids, c.ID[:min(len(c.ID), 12)])
		}
		sort.Strings(ids)
		return nil, &ce.Error{Code: ce.CodeAmbiguous, Op: "calendar.resolve",
			Message: "ambiguous calendar ID prefix: " + ref,
			Hint:    "candidates: " + strings.Join(ids, ", ")}
	}
	return nil, ce.NotFound("calendar.resolve", "calendar "+ref)
}

// DefaultCalendar resolves --calendar, then config, then the first writable calendar.
func (s *Service) DefaultCalendar(ctx context.Context, ref string) (*calendar.Calendar, error) {
	if ref != "" {
		c, err := s.ResolveCalendar(ctx, ref)
		if err != nil {
			return nil, err
		}
		if !c.Writable {
			// Do not embed titles in diagnostics: they are private calendar
			// payloads and the error model promises never to expose them.
			return nil, &ce.Error{Code: ce.CodeInvalidInput, Op: "calendar.default",
				Message: "the default calendar is read-only",
				Hint:    "choose a writable calendar with --calendar"}
		}
		return c, nil
	}
	if s.cfg.DefaultCalendar != "" {
		return s.ResolveCalendar(ctx, s.cfg.DefaultCalendar)
	}
	items, err := s.provider.ListCalendars(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].Writable {
			return &items[i], nil
		}
	}
	return nil, &ce.Error{Code: ce.CodeUnavailable, Op: "calendar.default",
		Message: "no writable calendar found",
		Hint:    "create one with `calendarctl calendar create \"Personal\"`"}
}

// ResolveEvent accepts an exact ID or a unique prefix.
func (s *Service) ResolveEvent(ctx context.Context, ref string) (*calendar.Event, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, ce.InvalidInput("event.resolve", "empty event reference")
	}
	if ev, err := s.provider.GetEvent(ctx, ref); err == nil && ev != nil {
		return ev, nil
	}
	// Search a generous window so short prefixes resolve for older events.
	now := s.Now()
	from := now.AddDate(-2, 0, 0)
	to := now.AddDate(2, 0, 0)
	items, err := s.provider.ListEvents(ctx, calendar.EventQuery{From: from, To: to})
	if err != nil {
		return nil, err
	}
	var matches []*calendar.Event
	for i := range items {
		if strings.HasPrefix(strings.ToLower(items[i].ID), strings.ToLower(ref)) {
			matches = append(matches, &items[i])
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		ids := make([]string, 0, len(matches))
		for _, e := range matches {
			ids = append(ids, e.ID[:min(len(e.ID), 12)])
		}
		return nil, &ce.Error{Code: ce.CodeAmbiguous, Op: "event.resolve",
			Message: "ambiguous event ID prefix: " + ref, Hint: "candidates: " + strings.Join(ids, ", ")}
	}
	return nil, ce.NotFound("event.resolve", "event "+ref)
}

// BuildQuery converts CLI flags into a provider query.
func (s *Service) BuildQuery(ctx context.Context, opts calendar.QueryOptions) (calendar.EventQuery, error) {
	now := s.Now()
	var from, to time.Time
	count := 0
	flag := func(on bool) {
		if on {
			count++
		}
	}

	switch {
	case opts.From != "" || opts.To != "":
		count++
		if opts.From == "" || opts.To == "" {
			return calendar.EventQuery{}, ce.InvalidInput("event.list", "--from and --to must be used together")
		}
		var err error
		from, err = timeutil.Parse(opts.From, s.loc, now, false)
		if err != nil {
			return calendar.EventQuery{}, ce.InvalidInput("event.list", "--from: "+err.Error())
		}
		to, err = timeutil.Parse(opts.To, s.loc, now, true)
		if err != nil {
			return calendar.EventQuery{}, ce.InvalidInput("event.list", "--to: "+err.Error())
		}
	case opts.Today:
		from, to = timeutil.DayStart(now, s.loc), timeutil.DayEnd(now, s.loc)
	case opts.Tomorrow:
		t := now.AddDate(0, 0, 1)
		from, to = timeutil.DayStart(t, s.loc), timeutil.DayEnd(t, s.loc)
	case opts.Week:
		from, to = timeutil.WeekRange(now, s.loc)
	case opts.Month:
		from, to = timeutil.MonthRange(now, s.loc)
	default:
		from = timeutil.DayStart(now.AddDate(0, 0, -30), s.loc)
		to = timeutil.DayEnd(now.AddDate(0, 0, 60), s.loc)
	}
	flag(opts.Today)
	flag(opts.Tomorrow)
	flag(opts.Week)
	flag(opts.Month)
	if count > 1 {
		return calendar.EventQuery{}, ce.InvalidInput("event.list", "choose only one of --today, --tomorrow, --week, --month or --from/--to")
	}
	if !to.After(from) {
		return calendar.EventQuery{}, ce.InvalidInput("event.list", "end of range must be after start")
	}
	q := calendar.EventQuery{From: from, To: to, Search: opts.Search, Limit: opts.Limit}
	if opts.Calendar != "" {
		c, err := s.ResolveCalendar(ctx, opts.Calendar)
		if err != nil {
			return calendar.EventQuery{}, err
		}
		q.CalendarID = c.ID
	}
	return q, nil
}

// FindDuplicate looks for an existing event with the same title, calendar and
// start/end (or an overlapping occurrence of the same recurring series).
func (s *Service) FindDuplicate(ctx context.Context, in calendar.EventInput) (*calendar.Event, error) {
	from := in.Start.Add(-2 * time.Hour)
	to := in.End.Add(2 * time.Hour)
	items, err := s.provider.ListEvents(ctx, calendar.EventQuery{From: from, To: to, CalendarID: in.CalendarID})
	if err != nil {
		return nil, err
	}
	for i := range items {
		e := &items[i]
		if !strings.EqualFold(strings.TrimSpace(e.Title), strings.TrimSpace(in.Title)) {
			continue
		}
		if e.Start.Equal(in.Start) && e.End.Equal(in.End) {
			return e, nil
		}
		if e.Recurrence != nil && in.Recurrence != nil &&
			strings.EqualFold(e.Recurrence.Frequency, in.Recurrence.Frequency) &&
			e.Start.Equal(in.Start) {
			return e, nil
		}
	}
	return nil, nil
}

// CreateEvent applies duplicate detection before writing.
func (s *Service) CreateEvent(ctx context.Context, in calendar.EventInput, allowDuplicate bool) (*calendar.Event, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, ce.InvalidInput("event.create", "--title is required")
	}
	if in.End.Before(in.Start) || in.End.Equal(in.Start) {
		return nil, ce.InvalidInput("event.create", "--end must be after --start")
	}
	if in.TimeZone == "" {
		in.TimeZone = s.cfg.TimezoneName()
	}
	if !allowDuplicate {
		dup, err := s.FindDuplicate(ctx, in)
		if err != nil {
			return nil, err
		}
		if dup != nil {
			return nil, &ce.Error{
				Code:    ce.CodeDuplicate,
				Op:      "event.create",
				Message: fmt.Sprintf("an event with this title and time already exists (%s)", shortID(dup.ID)),
				Hint:    "use --force to create it anyway",
			}
		}
	}
	return s.provider.CreateEvent(ctx, in)
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
