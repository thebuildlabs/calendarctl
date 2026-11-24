package apple

import (
	"time"

	"github.com/thebuildlabs/calendarctl/internal/calendar"
)

// Wire types mirror the Swift helper's JSON exactly. All timestamps are
// RFC3339 strings on the wire, even for all-day events (midnight local).

type wireCalendar struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Source     string `json:"source,omitempty"`
	Type       string `json:"type,omitempty"`
	Color      string `json:"color,omitempty"`
	Writable   bool   `json:"writable"`
	Subscribed bool   `json:"subscribed"`
	EventCount int    `json:"eventCount,omitempty"`
}

type wireRecurrence struct {
	Frequency  string   `json:"frequency,omitempty"`
	Interval   int      `json:"interval,omitempty"`
	Count      int      `json:"count,omitempty"`
	Until      string   `json:"until,omitempty"`
	DaysOfWeek []string `json:"daysOfWeek,omitempty"`
}

type wireReminder struct {
	MinutesBefore int    `json:"minutesBefore"`
	Message       string `json:"message,omitempty"`
}

type wireAttendee struct {
	Name   string `json:"name,omitempty"`
	Email  string `json:"email,omitempty"`
	Status string `json:"status,omitempty"`
}

type wireEvent struct {
	ID            string          `json:"id"`
	CalendarID    string          `json:"calendarId"`
	CalendarTitle string          `json:"calendar,omitempty"`
	Title         string          `json:"title"`
	Description   string          `json:"description,omitempty"`
	Location      string          `json:"location,omitempty"`
	URL           string          `json:"url,omitempty"`
	Start         string          `json:"start"`
	End           string          `json:"end"`
	AllDay        bool            `json:"allDay"`
	TimeZone      string          `json:"timeZone,omitempty"`
	Recurrence    *wireRecurrence `json:"recurrence,omitempty"`
	Reminders     []wireReminder  `json:"reminders,omitempty"`
	Attendees     []wireAttendee  `json:"attendees,omitempty"`
	Status        string          `json:"status,omitempty"`
	CreatedAt     string          `json:"createdAt,omitempty"`
	ModifiedAt    string          `json:"modifiedAt,omitempty"`
}

type wirePermission struct {
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func toModel(w wireEvent) calendar.Event {
	e := calendar.Event{
		ID:            w.ID,
		CalendarID:    w.CalendarID,
		CalendarTitle: w.CalendarTitle,
		Title:         w.Title,
		Description:   w.Description,
		Location:      w.Location,
		URL:           w.URL,
		Start:         parseTime(w.Start),
		End:           parseTime(w.End),
		AllDay:        w.AllDay,
		TimeZone:      w.TimeZone,
		Status:        w.Status,
	}
	if w.Recurrence != nil && w.Recurrence.Frequency != "" {
		r := &calendar.Recurrence{
			Frequency:  w.Recurrence.Frequency,
			Interval:   w.Recurrence.Interval,
			Count:      w.Recurrence.Count,
			DaysOfWeek: w.Recurrence.DaysOfWeek,
		}
		if w.Recurrence.Until != "" {
			t := parseTime(w.Recurrence.Until)
			r.Until = &t
		}
		e.Recurrence = r
	}
	for _, rem := range w.Reminders {
		e.Reminders = append(e.Reminders, calendar.Reminder{MinutesBefore: rem.MinutesBefore, Message: rem.Message})
	}
	for _, a := range w.Attendees {
		e.Attendees = append(e.Attendees, calendar.Attendee{Name: a.Name, Email: a.Email, Status: a.Status})
	}
	if t := parseTime(w.CreatedAt); !t.IsZero() {
		e.CreatedAt = &t
	}
	if t := parseTime(w.ModifiedAt); !t.IsZero() {
		e.ModifiedAt = &t
	}
	return e
}

func fromRecurrence(r *calendar.Recurrence) *wireRecurrence {
	if r == nil {
		return nil
	}
	out := &wireRecurrence{
		Frequency:  r.Frequency,
		Interval:   r.Interval,
		Count:      r.Count,
		DaysOfWeek: r.DaysOfWeek,
	}
	if r.Until != nil {
		out.Until = formatTime(*r.Until)
	}
	return out
}

func fromReminders(rems []calendar.Reminder) []wireReminder {
	var out []wireReminder
	for _, r := range rems {
		out = append(out, wireReminder{MinutesBefore: r.MinutesBefore, Message: r.Message})
	}
	return out
}

func toModelCalendar(w wireCalendar) calendar.Calendar {
	return calendar.Calendar{
		ID:         w.ID,
		Title:      w.Title,
		Source:     w.Source,
		Type:       w.Type,
		Color:      w.Color,
		Writable:   w.Writable,
		Subscribed: w.Subscribed,
		EventCount: w.EventCount,
	}
}
