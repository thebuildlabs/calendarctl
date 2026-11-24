// Package timeutil centralizes all date/time parsing and range arithmetic.
// Day boundaries always use the configured location so DST transitions are
// handled by Go's time package rather than by manual hour arithmetic.
package timeutil

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	dateOnlyRE   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	dayOffsetRE  = regexp.MustCompile(`^([+-])(\d+)d$`)
	hmRE         = regexp.MustCompile(`^([01]?\d|2[0-3]):([0-5]\d)$`)
	weekdayNames = map[string]time.Weekday{
		"sun": time.Sunday, "sunday": time.Sunday,
		"mon": time.Monday, "monday": time.Monday,
		"tue": time.Tuesday, "tuesday": time.Tuesday,
		"wed": time.Wednesday, "wednesday": time.Wednesday,
		"thu": time.Thursday, "thursday": time.Thursday,
		"fri": time.Friday, "friday": time.Friday,
		"sat": time.Saturday, "saturday": time.Saturday,
	}
)

// Parse interprets the accepted date/time syntaxes.
// endOfDay controls the time chosen when only a date is given.
func Parse(value string, loc *time.Location, now time.Time, endOfDay bool) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty date/time value")
	}
	if loc == nil {
		loc = time.Local
	}
	lower := strings.ToLower(value)

	switch lower {
	case "now":
		return now.In(loc), nil
	case "today":
		return DayStart(now, loc), nil
	case "tomorrow":
		return DayStart(now.AddDate(0, 0, 1), loc), nil
	case "yesterday":
		return DayStart(now.AddDate(0, 0, -1), loc), nil
	}
	if m := dayOffsetRE.FindStringSubmatch(lower); m != nil {
		n, _ := strconv.Atoi(m[2])
		if m[1] == "-" {
			n = -n
		}
		return DayStart(now.AddDate(0, 0, n), loc), nil
	}
	if wd, ok := weekdayNames[lower]; ok {
		return NextWeekday(now, loc, wd), nil
	}

	// RFC3339 / RFC3339Nano with an explicit offset or Z.
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02T15:04:05", value); err == nil {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc), nil
	}
	if t, err := time.Parse("2006-01-02T15:04", value); err == nil {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, loc), nil
	}
	if t, err := time.Parse("2006-01-02 15:04:05", value); err == nil {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc), nil
	}
	if t, err := time.Parse("2006-01-02 15:04", value); err == nil {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, loc), nil
	}
	if dateOnlyRE.MatchString(value) {
		t, err := time.ParseInLocation("2006-01-02", value, loc)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid date %q", value)
		}
		if endOfDay {
			return DayEnd(t, loc), nil
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("unrecognized date/time %q; use RFC3339 (2026-11-26T10:00:00+05:30), YYYY-MM-DD, or today/tomorrow", value)
}

// DayStart returns midnight in loc for the day containing t.
func DayStart(t time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.Local
	}
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// DayEnd returns 23:59:59.999999999 in loc for the day containing t.
func DayEnd(t time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.Local
	}
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 23, 59, 59, int(time.Second-time.Nanosecond), loc)
}

// NextWeekday returns the next occurrence of wd (today if it matches).
func NextWeekday(t time.Time, loc *time.Location, wd time.Weekday) time.Time {
	start := DayStart(t, loc)
	delta := (int(wd) - int(start.Weekday()) + 7) % 7
	return start.AddDate(0, 0, delta)
}

// WeekRange returns Monday 00:00 through Sunday 23:59:59.999999999.
func WeekRange(t time.Time, loc *time.Location) (time.Time, time.Time) {
	start := DayStart(t, loc)
	offset := (int(start.Weekday()) - int(time.Monday) + 7) % 7
	start = start.AddDate(0, 0, -offset)
	return start, DayEnd(start.AddDate(0, 0, 6), loc)
}

// MonthRange returns the first and last instant of t's month.
func MonthRange(t time.Time, loc *time.Location) (time.Time, time.Time) {
	start := DayStart(t, loc)
	start = time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 1, 0).Add(-time.Nanosecond)
	return start, end
}

// AtTime combines the date of t with an HH:MM string in loc.
func AtTime(t time.Time, hm string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.Local
	}
	m := hmRE.FindStringSubmatch(strings.TrimSpace(hm))
	if m == nil {
		return time.Time{}, fmt.Errorf("invalid time %q (expected HH:MM)", hm)
	}
	h, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	y, mo, d := t.In(loc).Date()
	return time.Date(y, mo, d, h, mi, 0, 0, loc), nil
}

// RecurrenceEndDate parses --until into a date at end of day in loc.
func RecurrenceEndDate(value string, loc *time.Location, now time.Time) (time.Time, error) {
	return Parse(value, loc, now, true)
}

// FormatDuration renders a duration in the compact form used in summaries.
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return "0 minutes"
	}
	mins := int(math.Round(d.Minutes()))
	if mins < 60 {
		if mins == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", mins)
	}
	hours := mins / 60
	rem := mins % 60
	hourWord := "hours"
	if hours == 1 {
		hourWord = "hour"
	}
	if rem == 0 {
		return fmt.Sprintf("%d %s", hours, hourWord)
	}
	return fmt.Sprintf("%d %s %d minutes", hours, hourWord, rem)
}

// Overlaps reports whether [a1,a2) intersects [b1,b2).
func Overlaps(a1, a2, b1, b2 time.Time) bool {
	return a1.Before(b2) && b1.Before(a2)
}
