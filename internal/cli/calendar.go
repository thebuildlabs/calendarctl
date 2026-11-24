package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thebuildlabs/calendarctl/internal/output"
)

func (a *App) calendarCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "calendar",
		Short: "Manage calendars",
		Long:  "List, inspect, create and delete calendars available to the configured provider.",
	}
	cmd.AddCommand(a.calendarListCommand())
	cmd.AddCommand(a.calendarShowCommand())
	cmd.AddCommand(a.calendarCreateCommand())
	cmd.AddCommand(a.calendarDeleteCommand())
	return cmd
}

func (a *App) calendarListCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List available calendars",
		Aliases: []string{"ls"},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			items, err := a.Service().Provider().ListCalendars(cmd.Context())
			if err != nil {
				return err
			}
			defaultID := ""
			if a.cfg.DefaultCalendar != "" {
				if c, err := a.Service().ResolveCalendar(cmd.Context(), a.cfg.DefaultCalendar); err == nil {
					defaultID = c.ID
				}
			}
			for i := range items {
				items[i].IsDefault = items[i].ID == defaultID
			}
			r := a.Renderer()
			rows := make([][]string, 0, len(items))
			for _, c := range items {
				access := "read/write"
				if !c.Writable {
					access = "read-only"
				}
				name := c.Title
				if c.IsDefault {
					name += " *"
				}
				source := c.Source
				if source == "" {
					source = "—"
				}
				rows = append(rows, []string{shortID(c.ID), name, source, c.Type, access})
			}
			footer := fmt.Sprintf("%d calendars", len(items))
			if defaultID != "" {
				footer += " · * default"
			}
			return r.Render(items, output.Table{
				Title:   "CALENDARS",
				Headers: []string{"ID", "NAME", "SOURCE", "TYPE", "ACCESS"},
				Rows:    rows,
				Footer:  footer,
			})
		},
	}
}

func (a *App) calendarShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <calendar-id>",
		Short: "Show details for a calendar",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.Service().ResolveCalendar(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			writable := "yes"
			if !c.Writable {
				writable = "no"
			}
			rows := [][]string{
				{"ID", c.ID},
				{"Name", c.Title},
				{"Source", orDash(c.Source)},
				{"Type", orDash(c.Type)},
				{"Color", orDash(c.Color)},
				{"Writable", writable},
				{"Subscribed", fmt.Sprintf("%t", c.Subscribed)},
			}
			return a.Renderer().Render(c, output.Table{
				Title:   strings.ToUpper(c.Title),
				Headers: []string{"FIELD", "VALUE"},
				Rows:    rows,
			})
		},
	}
}

func (a *App) calendarCreateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new local calendar",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.TrimSpace(args[0])
			if name == "" {
				return ceInvalid("calendar name cannot be empty")
			}
			c, err := a.Service().Provider().CreateCalendar(cmd.Context(), name)
			if err != nil {
				return err
			}
			r := a.Renderer()
			if err := r.Render(c, output.Table{
				Title:   "CALENDAR CREATED",
				Headers: []string{"FIELD", "VALUE"},
				Rows:    [][]string{{"ID", c.ID}, {"Name", c.Title}},
			}); err != nil {
				return err
			}
			if r.Format == "table" {
				return success(r, "Created calendar "+c.Title+" ("+shortID(c.ID)+")")
			}
			return nil
		},
	}
}

func (a *App) calendarDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <calendar-id>",
		Short: "Delete a calendar and all its events",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.Service().ResolveCalendar(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			ok, err := a.confirm(fmt.Sprintf("Delete calendar %q and all of its events?", c.Title))
			if err != nil {
				return err
			}
			if !ok {
				return ceCancelled()
			}
			if err := a.Service().Provider().DeleteCalendar(cmd.Context(), c.ID); err != nil {
				return err
			}
			return success(a.Renderer(), "Deleted calendar "+c.Title)
		},
	}
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
