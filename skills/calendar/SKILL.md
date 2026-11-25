---
name: calendar
description: Manage Apple Calendar events, calendars, and availability with the calendarctl CLI. Use when the user asks to list, search, create, update, or delete calendar events, find free meeting time, inspect calendars, or configure calendar permissions.
---

# Calendar Skill

Use `calendarctl` for all calendar work. Never try to manipulate Calendar.app
directly, and never ask the user for calendar credentials — authorization is
owned by macOS through EventKit.

## Before you start

Check that the CLI is available and onboarded:

```sh
command -v calendarctl
calendarctl permissions check
calendarctl config path
```

- If `permissions check` says `notDetermined` or `denied`, tell the user to run
  `calendarctl init` from a real Terminal window and, if needed, enable their
  terminal app under **System Settings → Privacy & Security → Calendars**.
  Do not attempt to bypass this macOS TCC prompt.
- If the CLI is missing, the user can install it with
  `brew install thebuildlabs/tap/calendarctl`, or from source with
  `make build && sudo make install` in the calendarctl repository.
- For safe demos, tests, or dry exploration without touching real calendars,
  add `--provider mock` to any command.

## General rules

- Always pass `--json` (or `--output json`) when you need to parse output.
  Human tables are for display only.
- Results go to stdout; errors and hints go to stderr. Check exit codes.
- Use `--yes` only for destructive commands and only when the user has
  explicitly confirmed the action.
- Prefer read-only commands first: `calendar list`, `event list`, `event search`,
  then make changes.
- Show the user what you are about to create/change/delete before doing it.
- Never print or log event titles, notes, attendees, or locations unless the
  user asks for them — treat calendar content as private.
- `--verbose` logging contains only IDs, counts, and durations.

## Reading calendars and events

```sh
calendarctl calendar list --json
calendarctl calendar show Work                 # ID prefix or exact title
calendarctl event list --today
calendarctl event list --week --calendar Work
calendarctl event list --from 2026-11-25 --to 2026-11-30
calendarctl event search "standup" --json
calendarctl event show evt-00101
```

`event list` supports `--today`, `--tomorrow`, `--week`, `--month`, and
`--from` + `--to` (used together). Add `--calendar`, `--search`, or `--limit`
to narrow results. Without a range flag it shows a window from 30 days ago to
60 days ahead.

## Creating events

```sh
calendarctl event create \
  --title "Project Meeting" \
  --start "2026-11-26T10:00:00+05:30" \
  --end "2026-11-26T11:00:00+05:30" \
  --calendar Work \
  --location "Room A" \
  --remind 15
```

Important create flags:

- `--title`, `--start` are required. `--end` defaults to +1 hour (+1 day for
  all-day events).
- `--calendar` defaults to the configured default calendar, then the first
  writable one.
- `--all-day` for all-day events; end date is exclusive.
- `--repeat daily|weekly|monthly|yearly`, plus `--interval`, `--count`,
  `--until`, and `--days mon,wed,fri` for weekly recurrence.
- `--remind N` can be repeated; `--no-default-reminders` skips config defaults.
- `--description`, `--url`, `--timezone` are also supported.

Duplicate detection compares normalized title, calendar, and exact time. A
duplicate exits with code **5**. Only use `--force` when the user explicitly
confirms they want a second copy.

Accepted date/time forms for `--start`, `--end`, `--from`, `--to`:

| Syntax | Example |
| --- | --- |
| RFC3339 with offset | `2026-11-26T10:00:00+05:30` |
| Local ISO 8601 | `2026-11-26T10:00` |
| Date only | `2026-11-26` |
| Relative | `today`, `tomorrow`, `+3d`, `monday` |
| Date + time | `2026-11-26 10:00` |

## Updating and deleting

```sh
calendarctl event update evt-00101 --title "Updated Meeting"
calendarctl event update evt-00101 --span future --location "Room B"
calendarctl event delete evt-00101           # prompts for confirmation
calendarctl event delete evt-00101 --yes     # only after explicit approval
```

- `event update` changes only flags you explicitly pass. Use an empty string
  to clear `--description`, `--location`, or `--url`.
- For recurring events, `--span this|future|all` controls scope. `this` creates
  an exception; `all` iterates the whole series.
- `calendar delete` deletes the calendar **and all its events** — always
  confirm explicitly and use `--yes` only then.
- Attendees are read-only in EventKit; do not promise to add or remove them.

## Finding free time

```sh
calendarctl availability today
calendarctl availability --date 2026-11-26 --duration 45 --json
calendarctl availability \
  --from 2026-11-26T09:00:00+05:30 \
  --to 2026-11-26T18:00:00+05:30 --duration 45
```

JSON output includes `events`, `busy`, `free`, `suggestions`, `eventCount`,
`busyMinutes`, and `freeMinutes`. Working hours come from config
(`workingHoursStart`, `workingHoursEnd`).

## Configuration and permissions

```sh
calendarctl config init
calendarctl config show
calendarctl config path
calendarctl permissions check
calendarctl permissions request
```

Config lives at `$XDG_CONFIG_HOME/calendarctl/config.yaml` or
`~/Library/Application Support/calendarctl/config.yaml`. Override values with
`CALENDARCTL_*` environment variables (for example
`CALENDARCTL_OUTPUT_FORMAT=json`). Never write credentials or tokens into the
config file.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | General error |
| 2 | Usage error |
| 3 | Not found |
| 4 | Permission denied |
| 5 | Already exists / duplicate |
| 6 | Cancelled by user |
| 7 | Provider unavailable |
| 8 | Invalid input |

When a command fails, surface the error and its hint to the user rather than
guessing or retrying with destructive flags.

## Recommended workflow

1. **Understand** — `calendar list --json`, then `event list` / `event search`.
2. **Plan** — summarize the proposed change and confirm with the user.
3. **Act** — run the create/update/delete command with explicit flags.
4. **Verify** — re-run the relevant read command with `--json` and report the
   result.
