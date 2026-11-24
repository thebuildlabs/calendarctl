package calendar

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Calendar describes a calendar source exposed by a provider.
type Calendar struct {
	ID         string `json:"id" yaml:"id"`
	Title      string `json:"title" yaml:"title"`
	Source     string `json:"source,omitempty" yaml:"source,omitempty"`
	Type       string `json:"type,omitempty" yaml:"type,omitempty"` // local, icloud, caldav, exchange, subscribed, birthdays
	Color      string `json:"color,omitempty" yaml:"color,omitempty"`
	Writable   bool   `json:"writable" yaml:"writable"`
	Subscribed bool   `json:"subscribed" yaml:"subscribed"`
	EventCount int    `json:"eventCount,omitempty" yaml:"eventCount,omitempty"`
	IsDefault  bool   `json:"isDefault,omitempty" yaml:"isDefault,omitempty"`
}

// Recurrence describes a repeating rule.
type Recurrence struct {
	Frequency  string     `json:"frequency" yaml:"frequency"` // daily, weekly, monthly, yearly
	Interval   int        `json:"interval,omitempty" yaml:"interval,omitempty"`
	Count      int        `json:"count,omitempty" yaml:"count,omitempty"`
	Until      *time.Time `json:"until,omitempty" yaml:"until,omitempty"`
	DaysOfWeek []string   `json:"daysOfWeek,omitempty" yaml:"daysOfWeek,omitempty"`
}

func (r *Recurrence) String() string {
	if r == nil {
		return ""
	}
	freq := r.Frequency
	if r.Interval > 1 {
		freq = fmt.Sprintf("every %d %ss", r.Interval, freq)
	}
	if r.Until != nil {
		return fmt.Sprintf("%s until %s", freq, r.Until.Format("2006-01-02"))
	}
	if r.Count > 0 {
		return fmt.Sprintf("%s (%d times)", freq, r.Count)
	}
	return freq
}

// Reminder is a relative alarm attached to an event.
type Reminder struct {
	MinutesBefore int    `json:"minutesBefore" yaml:"minutesBefore"`
	Message       string `json:"message,omitempty" yaml:"message,omitempty"`
}

// Attendee is a participant (read-only on Apple Calendar).
type Attendee struct {
	Name   string `json:"name,omitempty" yaml:"name,omitempty"`
	Email  string `json:"email,omitempty" yaml:"email,omitempty"`
	Status string `json:"status,omitempty" yaml:"status,omitempty"`
}

// Event is the provider-agnostic event model.
type Event struct {
	ID            string      `json:"id" yaml:"id"`
	CalendarID    string      `json:"calendarId" yaml:"calendarId"`
	CalendarTitle string      `json:"calendar,omitempty" yaml:"calendar,omitempty"`
	Title         string      `json:"title" yaml:"title"`
	Description   string      `json:"description,omitempty" yaml:"description,omitempty"`
	Location      string      `json:"location,omitempty" yaml:"location,omitempty"`
	URL           string      `json:"url,omitempty" yaml:"url,omitempty"`
	Start         time.Time   `json:"start" yaml:"start"`
	End           time.Time   `json:"end" yaml:"end"`
	AllDay        bool        `json:"allDay" yaml:"allDay"`
	TimeZone      string      `json:"timeZone,omitempty" yaml:"timeZone,omitempty"`
	Recurrence    *Recurrence `json:"recurrence,omitempty" yaml:"recurrence,omitempty"`
	Reminders     []Reminder  `json:"reminders,omitempty" yaml:"reminders,omitempty"`
	Attendees     []Attendee  `json:"attendees,omitempty" yaml:"attendees,omitempty"`
	Status        string      `json:"status,omitempty" yaml:"status,omitempty"`
	CreatedAt     *time.Time  `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	ModifiedAt    *time.Time  `json:"modifiedAt,omitempty" yaml:"modifiedAt,omitempty"`
}

// Duration returns the event duration. All-day events count full days.
func (e Event) Duration() time.Duration {
	if e.AllDay {
		start := time.Date(e.Start.Year(), e.Start.Month(), e.Start.Day(), 0, 0, 0, 0, e.Start.Location())
		end := time.Date(e.End.Year(), e.End.Month(), e.End.Day(), 0, 0, 0, 0, e.End.Location())
		if !end.After(start) {
			end = start.AddDate(0, 0, 1)
		}
		return end.Sub(start)
	}
	if e.End.Before(e.Start) {
		return 0
	}
	return e.End.Sub(e.Start)
}

// EventInput is the payload for creating an event.
type EventInput struct {
	CalendarID  string
	Title       string
	Description string
	Location    string
	URL         string
	Start       time.Time
	End         time.Time
	AllDay      bool
	TimeZone    string
	Recurrence  *Recurrence
	Reminders   []Reminder
}

// EventPatch is the payload for updating an event. Pointer fields mean
// "leave unchanged" when nil; empty strings clear the value.
type EventPatch struct {
	CalendarID      *string
	Title           *string
	Description     *string
	Location        *string
	URL             *string
	Start           *time.Time
	End             *time.Time
	AllDay          *bool
	TimeZone        *string
	Recurrence      *Recurrence
	ClearRecurrence bool
	Reminders       *[]Reminder
	Span            string // this | future | all
}

// EventQuery selects events for listing/searching.
type EventQuery struct {
	From       time.Time
	To         time.Time
	CalendarID string
	Search     string
	Limit      int
}

// PermissionStatus describes native calendar authorization state.
type PermissionStatus struct {
	State   string `json:"state" yaml:"state"` // authorized | writeOnly | denied | restricted | notDetermined | unavailable
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
}

// IsReadable reports whether events can be read.
func (p PermissionStatus) IsReadable() bool { return p.State == "authorized" }

// UnmarshalJSON is the inverse of MarshalJSON: date-only strings are accepted
// for all-day events so persisted stores and piped JSON round-trip correctly.
func (e *Event) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID            string          `json:"id"`
		CalendarID    string          `json:"calendarId"`
		CalendarTitle string          `json:"calendar,omitempty"`
		Title         string          `json:"title"`
		Description   string          `json:"description,omitempty"`
		Location      string          `json:"location,omitempty"`
		URL           string          `json:"url,omitempty"`
		Start         json.RawMessage `json:"start"`
		End           json.RawMessage `json:"end"`
		AllDay        bool            `json:"allDay"`
		TimeZone      string          `json:"timeZone,omitempty"`
		Recurrence    struct {
			Frequency  string   `json:"frequency"`
			Interval   int      `json:"interval,omitempty"`
			Count      int      `json:"count,omitempty"`
			Until      *string  `json:"until,omitempty"`
			DaysOfWeek []string `json:"daysOfWeek,omitempty"`
		} `json:"recurrence,omitempty"`
		Reminders  []Reminder `json:"reminders,omitempty"`
		Attendees  []Attendee `json:"attendees,omitempty"`
		Status     string     `json:"status,omitempty"`
		CreatedAt  *string    `json:"createdAt,omitempty"`
		ModifiedAt *string    `json:"modifiedAt,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	start, err := parseFlexibleTime(raw.Start, raw.AllDay)
	if err != nil {
		return err
	}
	end, err := parseFlexibleTime(raw.End, raw.AllDay)
	if err != nil {
		return err
	}
	*e = Event{
		ID: raw.ID, CalendarID: raw.CalendarID, CalendarTitle: raw.CalendarTitle,
		Title: raw.Title, Description: raw.Description, Location: raw.Location,
		URL: raw.URL, Start: start, End: end, AllDay: raw.AllDay,
		TimeZone: raw.TimeZone, Reminders: raw.Reminders, Attendees: raw.Attendees,
		Status: raw.Status,
	}
	if raw.Recurrence.Frequency != "" {
		r := &Recurrence{
			Frequency: raw.Recurrence.Frequency, Interval: raw.Recurrence.Interval,
			Count: raw.Recurrence.Count, DaysOfWeek: raw.Recurrence.DaysOfWeek,
		}
		if raw.Recurrence.Until != nil {
			t, err := time.Parse(time.RFC3339Nano, *raw.Recurrence.Until)
			if err != nil {
				return err
			}
			r.Until = &t
		}
		e.Recurrence = r
	}
	if raw.CreatedAt != nil {
		t, err := time.Parse(time.RFC3339Nano, *raw.CreatedAt)
		if err != nil {
			return err
		}
		e.CreatedAt = &t
	}
	if raw.ModifiedAt != nil {
		t, err := time.Parse(time.RFC3339Nano, *raw.ModifiedAt)
		if err != nil {
			return err
		}
		e.ModifiedAt = &t
	}
	return nil
}

func parseFlexibleTime(raw json.RawMessage, allDay bool) (time.Time, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return time.Time{}, err
	}
	if s == "" {
		return time.Time{}, nil
	}
	if allDay && len(s) == 10 {
		return time.ParseInLocation("2006-01-02", s, time.UTC)
	}
	return time.Parse(time.RFC3339Nano, s)
}

// MarshalJSON on Event is customized so all-day events render date-only
// strings, which is what users and scripts expect.
func (e Event) MarshalJSON() ([]byte, error) {
	type alias Event
	out := struct {
		alias
		Start any `json:"start"`
		End   any `json:"end"`
	}{alias: alias(e)}
	if e.AllDay {
		out.Start = e.Start.Format("2006-01-02")
		out.End = e.End.Format("2006-01-02")
	} else {
		out.Start = e.Start
		out.End = e.End
	}
	return json.Marshal(out)
}

// Match reports whether an event matches a case-insensitive search term.
func (e Event) Match(term string) bool {
	if term == "" {
		return true
	}
	t := strings.ToLower(term)
	for _, field := range []string{e.Title, e.Description, e.Location, e.CalendarTitle, e.URL} {
		if strings.Contains(strings.ToLower(field), t) {
			return true
		}
	}
	return false
}
