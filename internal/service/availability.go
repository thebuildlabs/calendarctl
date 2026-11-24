package service

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/thebuildlabs/calendarctl/internal/calendar"
	"github.com/thebuildlabs/calendarctl/internal/timeutil"
)

// Slot is a half-open interval [Start, End).
type Slot struct {
	Start    time.Time     `json:"start" yaml:"start"`
	End      time.Time     `json:"end" yaml:"end"`
	Duration time.Duration `json:"durationMinutes" yaml:"durationMinutes"`
}

// Availability is the complete result of an availability query.
type Availability struct {
	From        time.Time        `json:"from" yaml:"from"`
	To          time.Time        `json:"to" yaml:"to"`
	Events      []calendar.Event `json:"events" yaml:"events"`
	Busy        []Slot           `json:"busy" yaml:"busy"`
	Free        []Slot           `json:"free" yaml:"free"`
	Suggestions []Slot           `json:"suggestions" yaml:"suggestions"`
	EventCount  int              `json:"eventCount" yaml:"eventCount"`
	BusyMinutes int              `json:"busyMinutes" yaml:"busyMinutes"`
	FreeMinutes int              `json:"freeMinutes" yaml:"freeMinutes"`
}

// AvailabilityOptions configures an availability query.
type AvailabilityOptions struct {
	From           time.Time
	To             time.Time
	CalendarID     string
	MeetingMinutes int
	WorkingStart   string
	WorkingEnd     string
	MaxSuggestions int
}

// Availability computes busy/free windows and suggested meeting times.
func (s *Service) Availability(ctx context.Context, opts AvailabilityOptions) (*Availability, error) {
	if opts.MeetingMinutes <= 0 {
		opts.MeetingMinutes = s.cfg.DefaultMeetingMinutes
	}
	if opts.MaxSuggestions <= 0 {
		opts.MaxSuggestions = 5
	}
	if opts.WorkingStart == "" {
		opts.WorkingStart = s.cfg.WorkingHoursStart
	}
	if opts.WorkingEnd == "" {
		opts.WorkingEnd = s.cfg.WorkingHoursEnd
	}

	events, err := s.provider.ListEvents(ctx, calendar.EventQuery{
		From: opts.From, To: opts.To, CalendarID: opts.CalendarID,
	})
	if err != nil {
		return nil, err
	}

	// Only keep events that actually overlap the requested window.
	relevant := make([]calendar.Event, 0, len(events))
	busyRaw := make([]Slot, 0, len(events))
	for _, e := range events {
		start, end := eventBounds(e)
		if !timeutil.Overlaps(start, end, opts.From, opts.To) {
			continue
		}
		clippedStart := maxTime(start, opts.From)
		clippedEnd := minTime(end, opts.To)
		if !clippedEnd.After(clippedStart) {
			continue
		}
		relevant = append(relevant, e)
		busyRaw = append(busyRaw, Slot{Start: clippedStart, End: clippedEnd})
	}
	sort.Slice(relevant, func(i, j int) bool { return relevant[i].Start.Before(relevant[j].Start) })
	busy := mergeSlots(busyRaw)
	free := freeSlots(busy, opts.From, opts.To)

	suggestions := s.suggest(relevant, free, opts)

	var busyTotal, freeTotal time.Duration
	for _, sl := range busy {
		busyTotal += sl.End.Sub(sl.Start)
	}
	for _, sl := range free {
		freeTotal += sl.End.Sub(sl.Start)
	}

	return &Availability{
		From:        opts.From,
		To:          opts.To,
		Events:      relevant,
		Busy:        busy,
		Free:        free,
		Suggestions: suggestions,
		EventCount:  len(relevant),
		BusyMinutes: int(math.Round(busyTotal.Minutes())),
		FreeMinutes: int(math.Round(freeTotal.Minutes())),
	}, nil
}

func eventBounds(e calendar.Event) (time.Time, time.Time) {
	if !e.AllDay {
		start, end := e.Start, e.End
		if !end.After(start) {
			end = start.Add(time.Hour)
		}
		return start, end
	}
	start := time.Date(e.Start.Year(), e.Start.Month(), e.Start.Day(), 0, 0, 0, 0, e.Start.Location())
	end := time.Date(e.End.Year(), e.End.Month(), e.End.Day(), 0, 0, 0, 0, e.End.Location())
	if !end.After(start) {
		end = start.AddDate(0, 0, 1)
	}
	return start, end
}

func mergeSlots(in []Slot) []Slot {
	if len(in) == 0 {
		return nil
	}
	sort.Slice(in, func(i, j int) bool { return in[i].Start.Before(in[j].Start) })
	var out []Slot
	cur := in[0]
	for _, sl := range in[1:] {
		if sl.Start.Before(cur.End) || sl.Start.Equal(cur.End) {
			if sl.End.After(cur.End) {
				cur.End = sl.End
			}
			continue
		}
		out = append(out, withDuration(cur))
		cur = sl
	}
	out = append(out, withDuration(cur))
	return out
}

func withDuration(s Slot) Slot {
	s.Duration = s.End.Sub(s.Start)
	return s
}

func freeSlots(busy []Slot, from, to time.Time) []Slot {
	var out []Slot
	cursor := from
	for _, sl := range busy {
		if sl.Start.After(cursor) {
			out = append(out, withDuration(Slot{Start: cursor, End: sl.Start}))
		}
		if sl.End.After(cursor) {
			cursor = sl.End
		}
	}
	if cursor.Before(to) {
		out = append(out, withDuration(Slot{Start: cursor, End: to}))
	}
	return out
}

// suggest finds gaps inside working hours that can fit a meeting.
func (s *Service) suggest(events []calendar.Event, free []Slot, opts AvailabilityOptions) []Slot {
	_ = events
	meeting := time.Duration(opts.MeetingMinutes) * time.Minute
	var suggestions []Slot

	// Iterate day by day across the requested window.
	day := timeutil.DayStart(opts.From, s.loc)
	last := timeutil.DayStart(opts.To, s.loc)
	for !day.After(last) && len(suggestions) < opts.MaxSuggestions {
		ws, err := timeutil.AtTime(day, opts.WorkingStart, s.loc)
		if err == nil {
			we, err2 := timeutil.AtTime(day, opts.WorkingEnd, s.loc)
			if err2 == nil && we.After(ws) {
				windowStart := maxTime(ws, opts.From)
				windowEnd := minTime(we, opts.To)
				for _, gap := range free {
					candidateStart := maxTime(gap.Start, windowStart)
					candidateEnd := minTime(gap.End, windowEnd)
					if !candidateEnd.After(candidateStart) || candidateEnd.Sub(candidateStart) < meeting {
						continue
					}
					// Align to meeting-minute boundaries when possible.
					aligned := alignUp(candidateStart, opts.MeetingMinutes, s.loc)
					if aligned.Before(candidateStart) || !aligned.Add(meeting).After(candidateEnd) {
						aligned = candidateStart
					}
					suggestions = append(suggestions, withDuration(Slot{Start: aligned, End: aligned.Add(meeting)}))
					if len(suggestions) >= opts.MaxSuggestions {
						break
					}
				}
			}
		}
		day = day.AddDate(0, 0, 1)
	}
	return suggestions
}

func alignUp(t time.Time, minutes int, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, loc)
	elapsed := t.Sub(midnight)
	step := time.Duration(minutes) * time.Minute
	remainder := elapsed % step
	if remainder == 0 {
		return t
	}
	return t.Add(step - remainder)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
