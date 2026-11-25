# calendar skill

An Agent Skill for using the `calendarctl` CLI safely: querying calendars,
creating and updating events, finding free time, and handling permissions.

## Install

Copy this folder into your agent's skills directory:

- **Claude Code:** `.claude/skills/calendar/`
- **OpenCode:** `.opencode/skills/calendar/`
- **Cursor:** `.cursor/skills/calendar/`
- **Other agents:** wherever portable Agent Skills (`SKILL.md`) are loaded.

Example:

```sh
mkdir -p .claude/skills
cp -R skills/calendar .claude/skills/calendar
```

The skill expects `calendarctl` to be installed and available on `PATH`. Run
`calendarctl init` once from a real Terminal to grant macOS Calendar access.
