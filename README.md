<div align="center">
  <h1>calendarctl</h1>
  <p>Modern calendar management for the terminal.</p>
</div>

<p align="center">
  <a href="https://github.com/thebuildlabs/calendarctl/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/thebuildlabs/calendarctl/ci.yml?style=flat-square&label=CI" /></a>
  <a href="https://github.com/thebuildlabs/calendarctl/releases"><img alt="Release" src="https://img.shields.io/github/v/release/thebuildlabs/calendarctl?style=flat-square" /></a>
  <a href="https://github.com/thebuildlabs/homebrew-tap"><img alt="Homebrew" src="https://img.shields.io/badge/Homebrew-thebuildlabs%2Ftap-orange?style=flat-square" /></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/thebuildlabs/calendarctl?style=flat-square" /></a>
</p>
<p align="center">
  <a href="https://pkg.go.dev/github.com/thebuildlabs/calendarctl"><img alt="Go Reference" src="https://pkg.go.dev/badge/github.com/thebuildlabs/calendarctl.svg" /></a>
  <a href="https://goreportcard.com/report/github.com/thebuildlabs/calendarctl"><img alt="Go Report Card" src="https://goreportcard.com/badge/github.com/thebuildlabs/calendarctl" /></a>
  <a href="https://github.com/thebuildlabs/calendarctl/blob/main/go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/thebuildlabs/calendarctl?style=flat-square" /></a>
  <a href="https://goreleaser.com/"><img alt="GoReleaser" src="https://img.shields.io/badge/GoReleaser-enabled-orange?style=flat-square" /></a>
</p>
<p align="center">
  <a href="https://github.com/thebuildlabs/calendarctl/actions/workflows/ci.yml"><img alt="macOS" src="https://img.shields.io/badge/macOS-supported-blue?style=flat-square" /></a>
  <a href="https://github.com/thebuildlabs/calendarctl/actions/workflows/ci.yml"><img alt="Linux" src="https://img.shields.io/badge/Linux-builds-green?style=flat-square" /></a>
  <a href="https://github.com/thebuildlabs/calendarctl/actions/workflows/ci.yml"><img alt="Windows" src="https://img.shields.io/badge/Windows-builds-green?style=flat-square" /></a>
</p>

---

## What is calendarctl?

A fast, scriptable CLI for Apple Calendar. List and search events, create and
update them, and find free time — without opening a GUI. A small Swift helper
talks to EventKit, so the Go binary stays pure Go and cross-compiles.

```console
$ calendarctl event list --today

TODAY · NOV 25, 2026
  TIME            TITLE             CALENDAR
  09:00 - 10:00   Team Standup      Work
  11:00 - 12:00   Project Review    Work
  14:00 - 15:00   Study Session     Personal

3 events · 6 hours available
```

## Features

- Predictable commands: `calendar`, `event`, `availability`, `config`, `permissions`
- Scriptable: `--json`, `--yaml`, `--quiet`, stable exit codes, data on stdout,
  errors on stderr
- Works with calendars already in Calendar.app — iCloud, Google, Exchange, local
- Safe: confirmation for destructive commands, duplicate detection, no private
  data in logs

## Install

```sh
brew install thebuildlabs/tap/calendarctl
```

From source (Go 1.26.11+, Xcode command line tools on macOS):

```sh
git clone https://github.com/thebuildlabs/calendarctl.git
cd calendarctl
make build && sudo make install
```

Release builds on macOS must be signed with a Developer ID Application
certificate. The helper pins that Team ID so an ad-hoc-signed impostor cannot
borrow the calendar grant. Local builds are ad-hoc signed and work from the
build directory; `make verify-signing` is release-only and fails on ad-hoc
builds by design.

For a manual release:

```sh
SIGN_IDENTITY="Developer ID Application: The Build Labs (TEAMID)" \
  make bridge verify-signing && goreleaser release --clean
```

The tagged-release workflow automates this. It requires four protected
repository secrets:

- `SIGN_IDENTITY` — the exact codesign identity name.
- `SIGNING_CERTIFICATE_P12_BASE64` — base64 of the `.p12` (certificate + key).
- `SIGNING_CERTIFICATE_PASSWORD` — password for the `.p12`.
- `KEYCHAIN_PASSWORD` — password for the ephemeral CI keychain.

The workflow imports the certificate into a temporary keychain, builds with
`SIGN_IDENTITY`, runs `make verify-signing` (which fails the release if the
embedded `CalendarctlTrustedTeamID` is empty or either binary is signed by a
different Team ID), then signs the GoReleaser artifact with the same identity.

## Quick start

```sh
calendarctl init                      # permissions + config + default calendar
calendarctl event list --today
calendarctl event search "standup"
calendarctl event create --title "Meeting" \
  --start 2026-11-26T10:00 --end 2026-11-26T11:00 --remind 15
calendarctl availability tomorrow --duration 45
calendarctl event list --today --json | jq -r '.[].title'
```

`init` requests calendar access on first run. If no dialog appears (SSH, IDE
runner, automation), run this from a real Terminal instead:

```sh
calendarctl permissions request
```

The helper only honors a permission-launch that carries a one-time token from
the CLI, so invoking it directly with `--request-permissions` is rejected. The
token separates other users and accidental launches; it is not a boundary
against other processes running as the same user (see
[architecture](docs/ARCHITECTURE.md#security-model)).

### Bridge overrides

`--bridge`, `CALENDARCTL_BRIDGE`, and `config.bridgePath` replace the bundled
helper. They receive every event title, note, location, attendee and ID, so the
CLI requires an explicit opt-in: an interactive `y` prompt, or
`--allow-bridge-override` for scripts. `--yes` does not authorize an override,
and the path is signed/ownership-checked and re-validated before each call.

On Linux and Windows, use the mock provider:
`calendarctl --provider mock event list --today`.

## License

[MIT](LICENSE) — Copyright 2026 The Build Labs
