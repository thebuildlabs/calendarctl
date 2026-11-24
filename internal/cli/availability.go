package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	ce "github.com/thebuildlabs/calendarctl/internal/errors"
	"github.com/thebuildlabs/calendarctl/internal/output"
	"github.com/thebuildlabs/calendarctl/internal/service"
	"github.com/thebuildlabs/calendarctl/internal/timeutil"
)

func (a *App) availabilityCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "availability",
		Short: "Check free and busy time",
		Long:  "Show busy slots, free slots and suggested meeting times for a date or range.",
		Example: `  calendarctl availability today
  calendarctl availability tomorrow
  calendarctl availability --date 2026-11-26
  calendarctl availability --from 2026-11-26T09:00:00+05:30 --to 2026-11-26T18:00:00+05:30`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runAvailability(cmd, "")
		},
	}
	f := cmd.Flags()
	f.String("date", "", "date to inspect (YYYY-MM-DD, today, tomorrow)")
	f.String("from", "", "start of range (RFC3339 or YYYY-MM-DD)")
	f.String("to", "", "end of range (RFC3339 or YYYY-MM-DD)")
	f.String("calendar", "", "limit to a calendar")
	f.Int("duration", 0, "meeting duration in minutes for suggestions")
	f.Int("limit", 5, "maximum number of suggestions")

	cmd.AddCommand(&cobra.Command{
		Use:   "today",
		Short: "Availability for today",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runAvailability(cmd, "today")
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "tomorrow",
		Short: "Availability for tomorrow",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.runAvailability(cmd, "tomorrow")
		},
	})
	return cmd
}

type availabilityRow struct {
	start time.Time
	kind  string
	time  string
	label string
}

func (a *App) runAvailability(cmd *cobra.Command, preset string) error {
	f := cmd.Flags()
	date, _ := f.GetString("date")
	fromStr, _ := f.GetString("from")
	toStr, _ := f.GetString("to")
	calRef, _ := f.GetString("calendar")
	duration, _ := f.GetInt("duration")
	limit, _ := f.GetInt("limit")

	// Subcommands do not inherit local flags from their parent, so read them
	// from the parent when available.
	if cmd.Parent() != nil && cmd.Parent().Name() == "availability" {
		pf := cmd.Parent().Flags()
		if date == "" {
			date, _ = pf.GetString("date")
		}
		if fromStr == "" {
			fromStr, _ = pf.GetString("from")
		}
		if toStr == "" {
			toStr, _ = pf.GetString("to")
		}
		if calRef == "" {
			calRef, _ = pf.GetString("calendar")
		}
		if duration == 0 {
			duration, _ = pf.GetInt("duration")
		}
		if limit == 5 {
			limit, _ = pf.GetInt("limit")
		}
	}

	loc := a.Service().Location()
	now := a.Service().Now()
	var from, to time.Time

	count := 0
	if preset != "" {
		count++
	}
	if date != "" {
		count++
	}
	if fromStr != "" || toStr != "" {
		count++
	}
	if count > 1 {
		return ce.InvalidInput("availability", "use one of a preset, --date, or --from/--to")
	}

	switch {
	case preset == "today" || date == "today":
		from, to = timeutil.DayStart(now, loc), timeutil.DayEnd(now, loc)
	case preset == "tomorrow" || date == "tomorrow":
		t := now.AddDate(0, 0, 1)
		from, to = timeutil.DayStart(t, loc), timeutil.DayEnd(t, loc)
	case date != "":
		t, err := timeutil.Parse(date, loc, now, false)
		if err != nil {
			return ce.InvalidInput("availability", "--date: "+err.Error())
		}
		from, to = timeutil.DayStart(t, loc), timeutil.DayEnd(t, loc)
	case fromStr != "" || toStr != "":
		if fromStr == "" || toStr == "" {
			return ce.InvalidInput("availability", "--from and --to must be used together")
		}
		var err error
		from, err = timeutil.Parse(fromStr, loc, now, false)
		if err != nil {
			return ce.InvalidInput("availability", "--from: "+err.Error())
		}
		to, err = timeutil.Parse(toStr, loc, now, true)
		if err != nil {
			return ce.InvalidInput("availability", "--to: "+err.Error())
		}
	default:
		from, to = timeutil.DayStart(now, loc), timeutil.DayEnd(now, loc)
	}
	if !to.After(from) {
		return ce.InvalidInput("availability", "end of range must be after start")
	}

	opts := service.AvailabilityOptions{
		From: from, To: to, MeetingMinutes: duration,
		WorkingStart: a.cfg.WorkingHoursStart, WorkingEnd: a.cfg.WorkingHoursEnd,
		MaxSuggestions: limit,
	}
	if calRef != "" {
		c, err := a.Service().ResolveCalendar(cmd.Context(), calRef)
		if err != nil {
			return err
		}
		opts.CalendarID = c.ID
	}
	result, err := a.Service().Availability(cmd.Context(), opts)
	if err != nil {
		return err
	}

	title := "AVAILABILITY · " + strings.ToUpper(from.In(loc).Format(a.cfg.DateFormat))
	if !sameDay(from, to) {
		title = "AVAILABILITY · " + strings.ToUpper(from.In(loc).Format("Jan 02")) + " – " + strings.ToUpper(to.In(loc).Format("Jan 02, 2006"))
	}
	style := a.Renderer().Style
	var rows [][]string
	var additionals []availabilityRow
	for _, sl := range result.Busy {
		additionals = append(additionals, availabilityRow{sl.Start, "BUSY", slotTime(sl, a.cfg.TimeFormat), ""})
	}
	for _, sl := range result.Free {
		additionals = append(additionals, availabilityRow{sl.Start, "FREE", slotTime(sl, a.cfg.TimeFormat), ""})
	}
	for _, sl := range result.Suggestions {
		additionals = append(additionals, availabilityRow{sl.Start, "SUGGESTED", slotTime(sl, a.cfg.TimeFormat), "open slot"})
	}
	// Busy rows carry the event title as detail.
	for _, e := range result.Events {
		for i := range additionals {
			if additionals[i].kind == "BUSY" && additionals[i].start.Equal(e.Start) {
				additionals[i].label = e.Title
				break
			}
		}
	}
	sortRows(additionals)
	for _, row := range additionals {
		kind := row.kind
		switch kind {
		case "BUSY":
			kind = style.Red(kind)
		case "FREE":
			kind = style.Green(kind)
		case "SUGGESTED":
			kind = style.Cyan(kind)
		}
		rows = append(rows, []string{kind, row.time, row.label})
	}
	footer := fmt.Sprintf("%d events · %s busy · %s free",
		result.EventCount, timeutil.FormatDuration(time.Duration(result.BusyMinutes)*time.Minute),
		timeutil.FormatDuration(time.Duration(result.FreeMinutes)*time.Minute))
	return a.Renderer().Render(result, output.Table{
		Title:   title,
		Headers: []string{"STATUS", "TIME", "EVENT"},
		Rows:    rows,
		Footer:  footer,
	})
}

func slotTime(s service.Slot, layout string) string {
	if s.Start.Day() != s.End.Day() || s.Start.Month() != s.End.Month() {
		return s.Start.Format("Jan 02 15:04") + " → " + s.End.Format("Jan 02 15:04")
	}
	return s.Start.Format(layout) + " - " + s.End.Format(layout)
}

func sortRows(rows []availabilityRow) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].start.Before(rows[j-1].start); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}
