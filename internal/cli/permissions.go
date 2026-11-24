package cli

import (
	"github.com/spf13/cobra"
	"github.com/thebuildlabs/calendarctl/internal/bridge"
	"github.com/thebuildlabs/calendarctl/internal/calendar"
	"github.com/thebuildlabs/calendarctl/internal/output"
)

func (a *App) permissionsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "permissions",
		Short: "Inspect and request calendar permissions",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "check",
		Short: "Show the current calendar authorization state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			status, err := a.Service().Provider().CheckPermissions(cmd.Context())
			if err != nil {
				return err
			}
			return a.Renderer().Render(status, output.Table{
				Title: "CALENDAR PERMISSIONS",
				Rows:  [][]string{{"State", status.State}, {"Details", orDash(status.Message)}},
			})
		},
	})
	requestCmd := &cobra.Command{
		Use:   "request",
		Short: "Trigger the native calendar permission prompt",
		Long:  "Prefer `calendarctl init` for first-time setup. This command is useful when\nyou only want to re-trigger the native calendar permission dialog.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			noOpen, _ := cmd.Flags().GetBool("no-open")
			if err := a.ensurePermissions(cmd.Context(), !noOpen); err != nil {
				return err
			}
			return success(a.Renderer(), "Calendar access granted")
		},
	}
	requestCmd.Flags().Bool("no-open", false, "do not launch the helper app to trigger the system prompt")
	cmd.AddCommand(requestCmd)
	return cmd
}

func appBundlePath(p calendar.Provider) string {
	if ap, ok := p.(interface{ BridgePath() string }); ok {
		return bridge.AppBundlePath(ap.BridgePath())
	}
	return ""
}
