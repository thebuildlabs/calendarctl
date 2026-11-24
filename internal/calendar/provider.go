package calendar

import "context"

// Provider is the single seam between the CLI and any calendar backend.
// Implementations must be safe for sequential use and must return the typed
// errors from internal/errors where applicable.
type Provider interface {
	Name() string

	// Permissions
	CheckPermissions(ctx context.Context) (*PermissionStatus, error)
	RequestPermissions(ctx context.Context) (*PermissionStatus, error)

	// Calendars
	ListCalendars(ctx context.Context) ([]Calendar, error)
	GetCalendar(ctx context.Context, id string) (*Calendar, error)
	CreateCalendar(ctx context.Context, title string) (*Calendar, error)
	DeleteCalendar(ctx context.Context, id string) error

	// Events
	ListEvents(ctx context.Context, q EventQuery) ([]Event, error)
	GetEvent(ctx context.Context, id string) (*Event, error)
	CreateEvent(ctx context.Context, in EventInput) (*Event, error)
	UpdateEvent(ctx context.Context, id string, patch EventPatch) (*Event, error)
	DeleteEvent(ctx context.Context, id string, span string) error

	Close() error
}
