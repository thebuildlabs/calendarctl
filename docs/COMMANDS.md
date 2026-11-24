# Command reference

## `init`

One-command onboarding. Run this first:

```sh
calendarctl init
calendarctl init --calendar Work --timezone Asia/Kolkata --format json
```

It requests calendar access (including the app-bundle fallback needed by
macOS), creates the default config file, and selects a default calendar —
prompting interactively when there are several, or picking the first writable
one in non-interactive environments. `--force` overwrites an existing config;
`--skip-permissions` skips the authorization step.

---

Global flags (available on every command):

| Flag | Description |
| --- | --- |
| `--json` | JSON output |
| `--yaml` | YAML output |
| `--output <table\|json\|yaml>` | Explicit output format |
| `--no-color` | Disable ANSI colors |
| `--quiet` | Suppress stdout output (exit status only) |
| `--verbose` | Structured debug logging to stderr |
| `--yes` | Skip confirmation prompts |
| `--config <path>` | Alternate config file |
| `--provider <apple\|mock\|auto>` | Provider override |
| `--bridge <path>` | Native bridge binary path |

Data commands write results to stdout and errors to stderr.

---

## `calendar list`

List available calendars.

```sh
calendarctl calendar list
calendarctl calendar list --json
```

Columns: ID (short), NAME, SOURCE, TYPE, ACCESS. `*` marks the configured
default. JSON/YAML includes full IDs.

## `calendar show <calendar-id>`

Show calendar details. Accepts a full ID, a unique prefix, or an exact title.

## `calendar create <name>`

Create a calendar on the first writable source (iCloud preferred, then local).

## `calendar delete <calendar-id>`

Delete a calendar **and all its events**. Requires confirmation, or `--yes`.

---

## `event list`

List events in a range.

| Flag | Description |
| --- | --- |
| `--today` | Today (default-like shortcut) |
| `--tomorrow` | Tomorrow |
| `--week` | Monday–Sunday of the current week |
| `--month` | Current month |
| `--from`, `--to` | Explicit range (must be used together) |
| `--calendar` | Filter by calendar ID/name |
| `--search` | Filter by text |
| `--limit` | Maximum results |

With no range flag, shows a window from 30 days ago to 60 days ahead.

```sh
calendarctl event list --today
calendarctl event list --from 2026-11-25 --to 2026-11-30 --calendar Work
```

## `event show <event-id>`

Show full event details. Accepts a full ID or unique prefix.

## `event create`

| Flag | Description |
| --- | --- |
| `--title` | Required event title |
| `--start` | Required start (RFC3339, date, relative token) |
| `--end` | End time; defaults to +1h (+1 day for all-day) |
| `--calendar` | Target calendar (default: configured/first writable) |
| `--description` | Notes |
| `--location` | Location |
| `--url` | Event URL |
| `--all-day` | All-day event (dates are exclusive at the end) |
| `--timezone` | IANA timezone |
| `--repeat` | `daily`, `weekly`, `monthly`, `yearly` |
| `--interval` | Recurrence interval (default 1) |
| `--count` | Number of occurrences |
| `--until` | Recurrence end date |
| `--days` | Weekdays for weekly recurrence (`mon,wed,fri`) |
| `--remind` | Reminder minutes before; repeatable |
| `--no-default-reminders` | Skip configured defaults |
| `--force` | Allow a duplicate event |

```sh
calendarctl event create --title "Sprint Planning" \
  --start "2026-11-26T10:00:00+05:30" \
  --end "2026-11-26T11:30:00+05:30" \
  --calendar Work --remind 15 --remind 60
```

Duplicate detection compares normalized title, calendar and exact time (or
same recurring series start) and exits **5** unless `--force` is given.

## `event update <event-id>`

Flags mirror `event create` (except `--force`/`--no-default-reminders`) plus:

| Flag | Description |
| --- | --- |
| `--clear-recurrence` | Remove the recurrence rule |
| `--span <this\|future\|all>` | Scope for recurring events (default `this`) |

Only flags explicitly provided are changed. Use empty strings to clear
`--description`, `--location`, or `--url`. Moving between calendars uses
`--calendar`.

## `event delete <event-id>`

Deletes an event after confirmation. `--span` controls recurrence scope;
`--yes` skips the prompt.

## `event search <query>`

Case-insensitive search over title, notes, location, calendar name and URL.
Optional `--calendar`, `--from`, `--to`, `--limit`.

---

## `availability`

Show busy slots, free slots and suggested meeting times.

| Flag | Description |
| --- | --- |
| `--date` | Date to inspect (`YYYY-MM-DD`, `today`, `tomorrow`) |
| `--from`, `--to` | Explicit range |
| `--calendar` | Limit to a calendar |
| `--duration` | Meeting length in minutes for suggestions |
| `--limit` | Max suggestions (default 5) |

Subcommands `availability today` and `availability tomorrow` are shortcuts.

```sh
calendarctl availability today
calendarctl availability --date 2026-11-26 --duration 45 --json
```

The JSON result contains `events`, `busy`, `free`, `suggestions`, `eventCount`,
`busyMinutes` and `freeMinutes`. All-day events occupy the full day.

---

## `config init|show|path`

Manage the YAML configuration file. `init --force` overwrites. `path` always
prints the resolved path, making it suitable for shell scripts.

## `permissions check|request`

Inspect or request native calendar authorization. `request` first tries a
direct EventKit request, then launches the bundled `EventKitBridge.app` through
LaunchServices so macOS can display the system prompt. `--no-open` skips the
app launch. If no dialog can be displayed (SSH, IDE runner, automation agent),
grant access under **System Settings → Privacy & Security → Calendars**, or run
`open <path>/EventKitBridge.app --args --request-permissions` from a Terminal.

## `version`

Print version, OS and architecture (structured output available).

---

## Shell completions

Cobra provides a hidden-by-default `completion` command:

```sh
calendarctl completion bash > /etc/bash_completion.d/calendarctl
calendarctl completion zsh  > "${fpath[1]}/_calendarctl"
calendarctl completion fish > ~/.config/fish/completions/calendarctl.fish
```
