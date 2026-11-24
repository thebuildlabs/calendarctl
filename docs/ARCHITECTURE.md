# Architecture

calendarctl uses a layered architecture. Dependencies point inward: the CLI
depends on the service layer, the service layer depends on the provider
interface, and concrete providers depend on external systems (EventKit or an
in-memory store).

```text
┌──────────────────────────────────────────────────────────────┐
│ cmd/calendarctl                                              │
│   Execute() → error → exit code                              │
└───────────────────────────┬──────────────────────────────────┘
                            │
┌───────────────────────────▼──────────────────────────────────┐
│ internal/cli (Cobra)                                         │
│   root · calendar · event · availability · config · permissions│
│   - parses flags                                             │
│   - calls service methods                                    │
│   - renders through internal/output                           │
└───────────────┬───────────────────────────┬──────────────────┘
                │                           │
    ┌───────────▼───────────┐   ┌───────────▼──────────────┐
    │ internal/service     │   │ internal/output          │
    │ resolution, ranges,  │   │ table / JSON / YAML,     │
    │ dedupe, availability │   │ TTY-aware ANSI styling   │
    └───────────┬───────────┘   └──────────────────────────┘
                │
    ┌───────────▼───────────┐
    │ internal/calendar    │
    │ Provider interface   │
    │ Models               │
    └───────────┬───────────┘
                │
    ┌───────────▼───────────────┐   ┌─────────────────────────┐
    │ internal/providers/apple  │   │ internal/providers/mock │
    │ JSON-RPC-ish bridge client│  │ in-memory, seeded       │
    └───────────┬───────────────┘   └─────────────────────────┘
                │ stdin/stdout JSON
    ┌───────────▼───────────────────────────────────────────┐
    │ native/apple/EventKitBridge.swift                     │
    │ EKEventStore · calendars · events · recurrence · alarms│
    │ Embedded Info.plist with usage descriptions           │
    └───────────────────────────────────────────────────────┘
```

## Why a separate native process?

EventKit is an Objective-C/Swift framework. Calling it from Go would require
cgo and tie the binary to macOS at link time. Instead:

- The Go binary stays pure Go (`CGO_ENABLED=0`) and cross-compiles.
- The Swift helper is a small, auditable program with one job.
- The boundary is a length-prefixed JSON frame on stdin and a newline-delimited
  JSON response on stdout; it is easy to test with a fake helper (see
  `internal/bridge/client_test.go`).
- A crash in EventKit cannot take down the CLI command arbitrarily — the Go
  side reports a clean `unavailable` error.

Request shape:

```json
{"command":"listEvents","params":{"from":"2026-11-25T00:00:00+05:30","to":"2026-11-25T23:59:59+05:30"}}
```

Response shape:

```json
{"ok":true,"data":[{ "id":"...", "title":"...", "start":"...", "end":"..." }]}
```

Errors use `{"ok":false,"error":{"code":"permission_denied","message":"...","hint":"..."}}`
and are mapped to typed Go errors by `bridge.mapWireError`.

Requests are prefixed with a 4-byte big-endian length. The helper reads only
that many bytes (rejecting frames over 1 MiB before buffering them) and the
client caps responses at 16 MiB. Both sides share the constants in
`internal/bridge/protocol.go`.

## Security model

The TCC calendar grant belongs to the helper binary, so the helper accepts
commands only from the CLI it shipped with:

- **Caller identity** — the helper uses `SecCodeCopyGuestWithAttributes(getppid())`
  for the kernel's view of its parent, plus an allowed-path check and a pinned
  code identity. Release builds embed a Developer ID Team ID; ad-hoc builds fall
  back to a stable signing identifier plus path anchoring.
- **Handshake** — a 32-byte nonce travels on inherited fd 3 and must also match
  the value in the request, so piping a command directly into the helper fails.
- **Helper selection** — `internal/bridge/path.go` accepts only same-user-owned,
  non-world-writable, correctly signed binaries with our signing identifier,
  discovered relative to the CLI executable (never the working directory). The
  file's device/inode is captured and re-checked immediately before each `exec`
  under a mutex, closing the resolve→execute TOCTOU window.
- **Overrides** — `--bridge`, `CALENDARCTL_BRIDGE` and `config.bridgePath` receive
  the full calendar payload. They require an explicit interactive `y` or
  `--allow-bridge-override` in scripts; `--yes` is not accepted. A warning is
  always written to stderr.
- **Permission launch** — `--request-permissions` needs a one-time 0600 token in
  a 0700 directory, opened with `O_NOFOLLOW` and consumed on use.
- **Release anchor** — `make verify-signing` fails if the shipped helper's
  `CalendarctlTrustedTeamID` is empty or if either binary carries a different
  Team ID. The tagged-release workflow runs it before publishing.

**Threat-model boundary:** the permission token and file modes separate other
users, not other processes of the *same* user. A same-user malicious process can
read the token and may attempt helper substitution; the kernel-level caller
identity check and the resolve→exec revalidation are the actual boundaries.
Full same-user isolation would require an XPC Mach service with a
`SecCode`/audit-session identity check on the caller.

## Provider contract

```go
type Provider interface {
    Name() string
    CheckPermissions(ctx context.Context) (*PermissionStatus, error)
    RequestPermissions(ctx context.Context) (*PermissionStatus, error)
    ListCalendars(ctx context.Context) ([]Calendar, error)
    GetCalendar(ctx context.Context, id string) (*Calendar, error)
    CreateCalendar(ctx context.Context, title string) (*Calendar, error)
    DeleteCalendar(ctx context.Context, id string) error
    ListEvents(ctx context.Context, q EventQuery) ([]Event, error)
    GetEvent(ctx context.Context, id string) (*Event, error)
    CreateEvent(ctx context.Context, in EventInput) (*Event, error)
    UpdateEvent(ctx context.Context, id string, patch EventPatch) (*Event, error)
    DeleteEvent(ctx context.Context, id string, span string) error
    Close() error
}
```

Adding a provider:

1. Create `internal/providers/<name>/` and implement the interface.
2. Map provider-specific errors onto `internal/errors.Code`.
3. Register the name in `App.initProvider` (`internal/cli/root.go`).
4. Add unit tests against the interface plus an integration test if practical.

No CLI command needs to change.

## Business logic in `internal/service`

- **ID resolution** — accepts full IDs, unique prefixes, or calendar titles.
  Ambiguous prefixes produce `CodeAmbiguous` with candidate IDs in the hint.
- **Default calendar** — `--calendar` → config `defaultCalendar` → first
  writable calendar.
- **Range building** — `--today`, `--tomorrow`, `--week`, `--month`, and
  `--from/--to` are mutually exclusive. Ranges are half-open `[from, to]`
  using end-of-day for date-only values.
- **Duplicate detection** — compares normalized title, calendar and exact
  start/end (or same recurring series start) before creating.
- **Availability** — gathers overlapping events, clips them to the requested
  window, merges overlapping busy intervals, computes the free complement,
  and suggests meeting-sized gaps inside working hours.

## Error and exit-code model

`internal/errors.Error` carries a stable `Code`, a human `Message`, an
optional `Hint`, and an `Op` for debugging. `ExitCode(err)` maps codes to
process statuses (3 not found, 4 permission, 5 duplicate, 8 invalid input,
...). `main.go` renders errors to stderr with terminal-aware coloring and
calls `os.Exit` with that status.

## Privacy

The logger is intentionally minimal. Only IDs, counts, byte lengths, durations
and command names are logged. Event titles, notes, locations, URLs and
attendees never reach stderr in verbose mode. Native errors are sanitized to
generic EventKit error codes in `throwNative`.

## Build and release

- `make build` — Go CLI + native bridge.
- `make check` — gofmt, vet, all tests.
- GoReleaser builds macOS archives with the bridge and other-platform archives
  without it, generates the Homebrew formula for `thebuildlabs/homebrew-tap`,
  and publishes GitHub releases on `v*` tags.
- GitHub Actions runs formatting, vet, tests (with race detector) and
  cross-compilation checks on every push/PR.
