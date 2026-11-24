package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/thebuildlabs/calendarctl/internal/calendar"
	ce "github.com/thebuildlabs/calendarctl/internal/errors"
	"github.com/thebuildlabs/calendarctl/internal/output"
	"github.com/thebuildlabs/calendarctl/internal/timeutil"
)

func (a *App) eventCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "event",
		Short: "Manage events",
		Long:  "Create, list, view, search, update and delete calendar events.",
	}
	cmd.AddCommand(a.eventListCommand())
	cmd.AddCommand(a.eventShowCommand())
	cmd.AddCommand(a.eventCreateCommand())
	cmd.AddCommand(a.eventUpdateCommand())
	cmd.AddCommand(a.eventDeleteCommand())
	cmd.AddCommand(a.eventSearchCommand())
	return cmd
}

// ---------- list ----------

func (a *App) eventListCommand() *cobra.Command {
	var opts calendar.QueryOptions
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List events in a date range",
		Aliases: []string{"ls"},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			q, err := a.Service().BuildQuery(cmd.Context(), opts)
			if err != nil {
				return err
			}
			events, err := a.Service().Provider().ListEvents(cmd.Context(), q)
			if err != nil {
				return err
			}
			r := a.Renderer()
			title, subtitle := a.rangeTitle(q)
			footer := fmt.Sprintf("%d %s", len(events), plural(len(events), "event", "events"))
			if len(events) > 0 {
				available := availableDuration(events, q.From, q.To)
				if available > 0 {
					footer += " · " + timeutil.FormatDuration(available) + " available"
				}
			}
			tbl := output.Table{
				Title:    title,
				Subtitle: subtitle,
				Footer:   footer,
			}
			multiDay := q.To.Sub(q.From) > 26*time.Hour
			if multiDay {
				tbl.Headers = []string{"DATE", "TIME", "TITLE", "CALENDAR"}
			} else {
				tbl.Headers = []string{"TIME", "TITLE", "CALENDAR"}
			}
			for _, e := range events {
				row := []string{formatEventTime(e, a.cfg.TimeFormat), e.Title, orDash(e.CalendarTitle)}
				if multiDay {
					row = append([]string{formatEventDate(e)}, row...)
				}
				if e.Recurrence != nil {
					row[len(row)-2] += " ↻"
				}
				tbl.Rows = append(tbl.Rows, row)
			}
			return r.Render(events, tbl)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&opts.Today, "today", false, "events for today")
	f.BoolVar(&opts.Tomorrow, "tomorrow", false, "events for tomorrow")
	f.BoolVar(&opts.Week, "week", false, "events for the current week")
	f.BoolVar(&opts.Month, "month", false, "events for the current month")
	f.StringVar(&opts.From, "from", "", "start of range (date or RFC3339)")
	f.StringVar(&opts.To, "to", "", "end of range (date or RFC3339)")
	f.StringVar(&opts.Calendar, "calendar", "", "filter by calendar (ID or name)")
	f.StringVar(&opts.Search, "search", "", "filter by text")
	f.IntVar(&opts.Limit, "limit", 0, "maximum number of events")
	return cmd
}

func (a *App) rangeTitle(q calendar.EventQuery) (string, string) {
	now := a.Service().Now()
	start := q.From.In(a.Service().Location())
	end := q.To.In(a.Service().Location())
	switch {
	case sameDay(start, now):
		return "TODAY · " + strings.ToUpper(start.Format(a.cfg.DateFormat)), "Events for today"
	case sameDay(start, now.AddDate(0, 0, 1)):
		return "TOMORROW · " + strings.ToUpper(start.Format(a.cfg.DateFormat)), "Events for tomorrow"
	case end.Sub(start) <= 8*24*time.Hour:
		return strings.ToUpper(start.Format("Mon Jan 02")) + " – " + strings.ToUpper(end.Format("Mon Jan 02")), "Events this week"
	default:
		return "EVENTS · " + strings.ToUpper(start.Format("Jan 2006")), "Events this month"
	}
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func availableDuration(events []calendar.Event, from, to time.Time) time.Duration {
	type span struct{ s, e time.Time }
	var spans []span
	for _, e := range events {
		s, end := e.Start, e.End
		if e.AllDay {
			s = time.Date(s.Year(), s.Month(), s.Day(), 0, 0, 0, 0, s.Location())
			end = time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 0, end.Location())
		}
		if end.Before(s) {
			continue
		}
		if s.Before(from) {
			s = from
		}
		if end.After(to) {
			end = to
		}
		if end.After(s) {
			spans = append(spans, span{s, end})
		}
	}
	if len(spans) == 0 {
		return to.Sub(from)
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].s.Before(spans[j].s) })
	var busy time.Duration
	cur := spans[0]
	for _, sp := range spans[1:] {
		if sp.s.Before(cur.e) || sp.s.Equal(cur.e) {
			if sp.e.After(cur.e) {
				cur.e = sp.e
			}
			continue
		}
		busy += cur.e.Sub(cur.s)
		cur = sp
	}
	busy += cur.e.Sub(cur.s)
	total := to.Sub(from)
	if busy > total {
		busy = total
	}
	return total - busy
}

// ---------- show ----------

func (a *App) eventShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <event-id>",
		Short: "Show full details for an event",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.Service().ResolveEvent(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			rows := [][]string{
				{"ID", e.ID},
				{"Title", e.Title},
				{"Calendar", orDash(e.CalendarTitle)},
				{"When", a.eventWhen(*e)},
				{"Timezone", orDash(e.TimeZone)},
			}
			if e.Location != "" {
				rows = append(rows, []string{"Location", e.Location})
			}
			if e.URL != "" {
				rows = append(rows, []string{"URL", e.URL})
			}
			if e.Recurrence != nil {
				rows = append(rows, []string{"Repeats", e.Recurrence.String()})
			}
			if len(e.Reminders) > 0 {
				var mins []string
				for _, rem := range e.Reminders {
					mins = append(mins, strconv.Itoa(rem.MinutesBefore)+"m before")
				}
				rows = append(rows, []string{"Reminders", strings.Join(mins, ", ")})
			}
			if len(e.Attendees) > 0 {
				var names []string
				for _, att := range e.Attendees {
					if att.Name != "" {
						names = append(names, att.Name)
					} else {
						names = append(names, att.Email)
					}
				}
				rows = append(rows, []string{"Attendees", strings.Join(names, ", ")})
			}
			if e.Description != "" {
				rows = append(rows, []string{"Description", e.Description})
			}
			return a.Renderer().Render(e, output.Table{
				Title:   strings.ToUpper(e.Title),
				Headers: []string{"FIELD", "VALUE"},
				Rows:    rows,
			})
		},
	}
}

func (a *App) eventWhen(e calendar.Event) string {
	loc := a.Service().Location()
	if e.AllDay {
		if sameDay(e.Start, e.End) || e.End.Sub(e.Start) <= 24*time.Hour {
			return e.Start.In(loc).Format("Mon Jan 02, 2006") + " (all-day)"
		}
		return e.Start.In(loc).Format("Mon Jan 02") + " – " + e.End.In(loc).Format("Mon Jan 02, 2006") + " (all-day)"
	}
	return e.Start.In(loc).Format("Mon Jan 02, 2006 15:04 -0700") + " → " + e.End.In(loc).Format("15:04 -0700")
}

// ---------- create ----------

type eventWriteFlags struct {
	title        string
	start        string
	end          string
	calendar     string
	description  string
	location     string
	url          string
	allDay       bool
	timezone     string
	repeat       string
	until        string
	count        int
	interval     int
	days         string
	reminders    []int
	noDefaultRem bool
	span         string
	clearRec     bool
	force        bool
}

func addEventWriteFlags(cmd *cobra.Command, f *eventWriteFlags, create bool) {
	fs := cmd.Flags()
	if create {
		fs.StringVar(&f.title, "title", "", "event title (required)")
		fs.StringVar(&f.start, "start", "", "start time (RFC3339, YYYY-MM-DD or relative)")
		fs.StringVar(&f.end, "end", "", "end time (RFC3339, YYYY-MM-DD or relative)")
		fs.StringVar(&f.calendar, "calendar", "", "target calendar (ID or name)")
		fs.StringVar(&f.description, "description", "", "event notes")
		fs.StringVar(&f.location, "location", "", "event location")
		fs.StringVar(&f.url, "url", "", "event URL")
		fs.BoolVar(&f.allDay, "all-day", false, "create an all-day event")
		fs.StringVar(&f.repeat, "repeat", "", "recurrence: daily, weekly, monthly, yearly")
		fs.StringVar(&f.until, "until", "", "recurrence end date")
		fs.IntVar(&f.count, "count", 0, "number of occurrences")
		fs.IntVar(&f.interval, "interval", 1, "recurrence interval")
		fs.StringVar(&f.days, "days", "", "weekdays for weekly recurrence (mon,wed,fri)")
		fs.IntSliceVar(&f.reminders, "remind", nil, "reminder in minutes before (repeatable)")
		fs.BoolVar(&f.noDefaultRem, "no-default-reminders", false, "do not apply configured default reminders")
		fs.BoolVar(&f.force, "force", false, "allow duplicate events")
	} else {
		fs.StringVar(&f.title, "title", "", "new title")
		fs.StringVar(&f.start, "start", "", "new start time")
		fs.StringVar(&f.end, "end", "", "new end time")
		fs.StringVar(&f.calendar, "calendar", "", "move event to this calendar")
		fs.StringVar(&f.description, "description", "", "new notes (use --description \"\" to clear)")
		fs.StringVar(&f.location, "location", "", "new location (use --location \"\" to clear)")
		fs.StringVar(&f.url, "url", "", "new URL (use --url \"\" to clear)")
		fs.BoolVar(&f.allDay, "all-day", false, "mark as all-day")
		fs.StringVar(&f.repeat, "repeat", "", "new recurrence rule")
		fs.StringVar(&f.until, "until", "", "new recurrence end date")
		fs.IntVar(&f.count, "count", 0, "new occurrence count")
		fs.IntVar(&f.interval, "interval", 1, "new recurrence interval")
		fs.IntSliceVar(&f.reminders, "remind", nil, "replace reminders (repeatable)")
		fs.StringVar(&f.span, "span", "this", "update scope: this, future or all")
		fs.BoolVar(&f.clearRec, "clear-recurrence", false, "remove recurrence")
	}
	fs.StringVar(&f.timezone, "timezone", "", "IANA timezone (default: configured timezone)")
}

func (a *App) parseRecurrence(f *eventWriteFlags, cmd *cobra.Command) (*calendar.Recurrence, error) {
	if f.repeat == "" && f.until == "" && f.count == 0 {
		return nil, nil
	}
	freq := strings.ToLower(strings.TrimSpace(f.repeat))
	if freq == "" {
		return nil, ce.InvalidInput("event", "--until/--count require --repeat")
	}
	switch freq {
	case "daily", "weekly", "monthly", "yearly", "none":
	default:
		return nil, ce.InvalidInput("event", fmt.Sprintf("invalid --repeat %q (use daily, weekly, monthly or yearly)", f.repeat))
	}
	if freq == "none" {
		return nil, nil
	}
	r := &calendar.Recurrence{Frequency: freq, Interval: f.interval}
	if r.Interval < 1 {
		r.Interval = 1
	}
	if f.until != "" && f.count > 0 {
		return nil, ce.InvalidInput("event", "use either --until or --count, not both")
	}
	if f.until != "" {
		t, err := timeutil.RecurrenceEndDate(f.until, a.Service().Location(), a.Service().Now())
		if err != nil {
			return nil, ce.InvalidInput("event", "--until: "+err.Error())
		}
		r.Until = &t
	}
	if f.count > 0 {
		r.Count = f.count
	}
	if f.days != "" {
		valid := map[string]bool{"sun": true, "mon": true, "tue": true, "wed": true, "thu": true, "fri": true, "sat": true}
		for _, d := range strings.Split(strings.ToLower(f.days), ",") {
			d = strings.TrimSpace(d)
			if !valid[d] {
				return nil, ce.InvalidInput("event", "invalid weekday in --days: "+d)
			}
			r.DaysOfWeek = append(r.DaysOfWeek, d)
		}
	}
	_ = cmd
	return r, nil
}

func (a *App) parseReminders(f *eventWriteFlags) []calendar.Reminder {
	mins := f.reminders
	if len(mins) == 0 && !f.noDefaultRem {
		mins = a.cfg.DefaultReminderMins
	}
	if f.noDefaultRem && len(mins) == 0 {
		return nil
	}
	var out []calendar.Reminder
	seen := map[int]bool{}
	for _, m := range mins {
		if m < 0 || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, calendar.Reminder{MinutesBefore: m})
	}
	return out
}

func (a *App) eventCreateCommand() *cobra.Command {
	var f eventWriteFlags
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new event",
		Example: `  calendarctl event create --title "Project Meeting" \
    --start "2026-11-26T10:00:00+05:30" --end "2026-11-26T11:00:00+05:30" --calendar Work

  calendarctl event create --title "Daily Standup" \
    --start "2026-11-26T09:00:00+05:30" --end "2026-11-26T09:30:00+05:30" \
    --repeat daily --until 2026-12-26`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(f.title) == "" {
				return ce.InvalidInput("event.create", "--title is required")
			}
			if f.start == "" {
				return ce.InvalidInput("event.create", "--start is required")
			}
			loc := a.Service().Location()
			now := a.Service().Now()
			start, err := timeutil.Parse(f.start, loc, now, false)
			if err != nil {
				return ce.InvalidInput("event.create", "--start: "+err.Error())
			}
			var end time.Time
			if f.end == "" {
				if f.allDay {
					end = start.AddDate(0, 0, 1)
				} else {
					end = start.Add(time.Hour)
				}
			} else {
				end, err = timeutil.Parse(f.end, loc, now, false)
				if err != nil {
					return ce.InvalidInput("event.create", "--end: "+err.Error())
				}
			}
			if f.allDay {
				start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
				if !end.After(start) {
					end = start.AddDate(0, 0, 1)
				}
				end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, loc)
			}
			if !end.After(start) {
				return ce.InvalidInput("event.create", "--end must be after --start")
			}
			cal, err := a.Service().DefaultCalendar(cmd.Context(), f.calendar)
			if err != nil {
				return err
			}
			rec, err := a.parseRecurrence(&f, cmd)
			if err != nil {
				return err
			}
			tz := f.timezone
			if tz == "" {
				tz = a.cfg.Timezone
			}
			if tz == "" {
				tz = a.cfg.TimezoneName()
			}
			in := calendar.EventInput{
				CalendarID: cal.ID, Title: strings.TrimSpace(f.title),
				Description: f.description, Location: f.location, URL: f.url,
				Start: start, End: end, AllDay: f.allDay, TimeZone: tz,
				Recurrence: rec, Reminders: a.parseReminders(&f),
			}
			created, err := a.Service().CreateEvent(cmd.Context(), in, f.force)
			if err != nil {
				return err
			}
			r := a.Renderer()
			if err := r.Render(created, output.Table{
				Title:   "EVENT CREATED",
				Headers: []string{"FIELD", "VALUE"},
				Rows: [][]string{
					{"ID", created.ID},
					{"Title", created.Title},
					{"When", a.eventWhen(*created)},
					{"Calendar", orDash(created.CalendarTitle)},
				},
			}); err != nil {
				return err
			}
			if r.Format == "table" {
				return success(r, "Created event "+created.Title+" ("+shortID(created.ID)+")")
			}
			return nil
		},
	}
	addEventWriteFlags(cmd, &f, true)
	return cmd
}

// ---------- update ----------

func (a *App) eventUpdateCommand() *cobra.Command {
	var f eventWriteFlags
	cmd := &cobra.Command{
		Use:   "update <event-id>",
		Short: "Update an existing event",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.Service().ResolveEvent(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if cmd.Flags().NFlag() == 0 {
				return ce.InvalidInput("event.update", "no changes specified")
			}
			loc := a.Service().Location()
			now := a.Service().Now()
			patch := calendar.EventPatch{Span: f.span}
			if cmd.Flags().Changed("title") {
				v := strings.TrimSpace(f.title)
				if v == "" {
					return ce.InvalidInput("event.update", "--title cannot be empty")
				}
				patch.Title = &v
			}
			if cmd.Flags().Changed("description") {
				patch.Description = &f.description
			}
			if cmd.Flags().Changed("location") {
				patch.Location = &f.location
			}
			if cmd.Flags().Changed("url") {
				patch.URL = &f.url
			}
			if cmd.Flags().Changed("all-day") {
				patch.AllDay = &f.allDay
			}
			if cmd.Flags().Changed("timezone") {
				patch.TimeZone = &f.timezone
			}
			if cmd.Flags().Changed("calendar") {
				c, err := a.Service().ResolveCalendar(cmd.Context(), f.calendar)
				if err != nil {
					return err
				}
				patch.CalendarID = &c.ID
			}
			if cmd.Flags().Changed("start") {
				t, err := timeutil.Parse(f.start, loc, now, false)
				if err != nil {
					return ce.InvalidInput("event.update", "--start: "+err.Error())
				}
				patch.Start = &t
			}
			if cmd.Flags().Changed("end") {
				t, err := timeutil.Parse(f.end, loc, now, false)
				if err != nil {
					return ce.InvalidInput("event.update", "--end: "+err.Error())
				}
				patch.End = &t
			}
			if cmd.Flags().Changed("remind") {
				rems := a.parseReminders(&f)
				patch.Reminders = &rems
			}
			if f.clearRec {
				patch.ClearRecurrence = true
			} else if rec, err := a.parseRecurrence(&f, cmd); err != nil {
				return err
			} else if rec != nil {
				patch.Recurrence = rec
			}
			span := strings.ToLower(f.span)
			if span != "this" && span != "future" && span != "all" {
				return ce.InvalidInput("event.update", "--span must be this, future or all")
			}
			patch.Span = span
			updated, err := a.Service().Provider().UpdateEvent(cmd.Context(), e.ID, patch)
			if err != nil {
				return err
			}
			r := a.Renderer()
			if err := r.Render(updated, output.Table{
				Title:   "EVENT UPDATED",
				Headers: []string{"FIELD", "VALUE"},
				Rows:    [][]string{{"ID", updated.ID}, {"Title", updated.Title}, {"When", a.eventWhen(*updated)}},
			}); err != nil {
				return err
			}
			if r.Format == "table" {
				return success(r, "Updated event ("+shortID(updated.ID)+")")
			}
			return nil
		},
	}
	addEventWriteFlags(cmd, &f, false)
	return cmd
}

// ---------- delete ----------

func (a *App) eventDeleteCommand() *cobra.Command {
	var span string
	cmd := &cobra.Command{
		Use:   "delete <event-id>",
		Short: "Delete an event",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := a.Service().ResolveEvent(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			s := strings.ToLower(span)
			if s != "this" && s != "future" && s != "all" {
				return ce.InvalidInput("event.delete", "--span must be this, future or all")
			}
			label := "event " + e.Title
			if e.Recurrence != nil {
				label += " (scope: " + s + ")"
			}
			ok, err := a.confirm("Delete " + label + "?")
			if err != nil {
				return err
			}
			if !ok {
				return ceCancelled()
			}
			if err := a.Service().Provider().DeleteEvent(cmd.Context(), e.ID, s); err != nil {
				return err
			}
			return success(a.Renderer(), "Deleted event "+e.Title+" ("+shortID(e.ID)+")")
		},
	}
	cmd.Flags().StringVar(&span, "span", "this", "deletion scope for recurring events: this, future or all")
	return cmd
}

// ---------- search ----------

func (a *App) eventSearchCommand() *cobra.Command {
	var calendarRef, from, to string
	var limit int
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search events by title, notes, location or calendar",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			loc := a.Service().Location()
			now := a.Service().Now()
			start := now.AddDate(-2, 0, 0)
			end := now.AddDate(2, 0, 0)
			if from != "" {
				t, err := timeutil.Parse(from, loc, now, false)
				if err != nil {
					return ce.InvalidInput("event.search", "--from: "+err.Error())
				}
				start = t
			}
			if to != "" {
				t, err := timeutil.Parse(to, loc, now, true)
				if err != nil {
					return ce.InvalidInput("event.search", "--to: "+err.Error())
				}
				end = t
			}
			q := calendar.EventQuery{From: start, To: end, Search: args[0], Limit: limit}
			if calendarRef != "" {
				c, err := a.Service().ResolveCalendar(cmd.Context(), calendarRef)
				if err != nil {
					return err
				}
				q.CalendarID = c.ID
			}
			events, err := a.Service().Provider().ListEvents(cmd.Context(), q)
			if err != nil {
				return err
			}
			// Provider-side filtering may be substring-only; apply model matching too.
			var matched []calendar.Event
			for _, e := range events {
				if e.Match(args[0]) {
					matched = append(matched, e)
				}
			}
			tbl := output.Table{
				Title:   "SEARCH · " + strings.ToUpper(args[0]),
				Headers: []string{"DATE", "TIME", "TITLE", "CALENDAR"},
				Footer:  fmt.Sprintf("%d %s", len(matched), plural(len(matched), "result", "results")),
			}
			for _, e := range matched {
				tbl.Rows = append(tbl.Rows, []string{
					formatEventDate(e), formatEventTime(e, a.cfg.TimeFormat), e.Title, orDash(e.CalendarTitle),
				})
			}
			return a.Renderer().Render(matched, tbl)
		},
	}
	cmd.Flags().StringVar(&calendarRef, "calendar", "", "limit to a calendar")
	cmd.Flags().StringVar(&from, "from", "", "search from this date/time")
	cmd.Flags().StringVar(&to, "to", "", "search up to this date/time")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum number of results")
	return cmd
}

func plural(n int, singular, many string) string {
	if n == 1 {
		return singular
	}
	return many
}
