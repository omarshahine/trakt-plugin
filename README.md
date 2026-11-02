# trakt-plugin

```
████████╗██████╗  █████╗ ██╗  ██╗████████╗     ██████╗██╗     ██╗
╚══██╔══╝██╔══██╗██╔══██╗██║ ██╔╝╚══██╔══╝    ██╔════╝██║     ██║
   ██║   ██████╔╝███████║█████╔╝    ██║       ██║     ██║     ██║
   ██║   ██╔══██╗██╔══██║██╔═██╗    ██║       ██║     ██║     ██║
   ██║   ██║  ██║██║  ██║██║  ██╗   ██║       ╚██████╗███████╗██║
   ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝   ╚═╝        ╚═════╝╚══════╝╚═╝
```

A CLI and AI agent plugin for [Trakt.tv](https://trakt.tv) using the [Trakt API](https://trakt.docs.apiary.io/). Search movies and shows, view and manage your watchlist (add/remove), view and edit watch history (add/remove), track show progress, check the upcoming-episodes calendar, and mark items as watched.

Ships as both a standalone Go CLI and as plugins for [OpenClaw](https://docs.openclaw.ai/) and [Claude Code](https://claude.ai/claude-code).

## Installation

### Go CLI

```bash
go install github.com/omarshahine/trakt-plugin@latest
```

Or grab a binary from the [releases](https://github.com/omarshahine/trakt-plugin/releases).

### OpenClaw Plugin

```bash
openclaw plugins install trakt-tools
```

### Claude Code Plugin

```bash
claude --plugin-dir /path/to/trakt-plugin
```

### From Source

```bash
git clone https://github.com/omarshahine/trakt-plugin
cd trakt-plugin
go build -o trakt-cli .
```

## Authentication

```
trakt-cli auth
Please go to https://trakt.tv/activate and enter the following code: XXXXXXXX
Successfully authenticated, creds written to ~/.trakt.yaml
```

The CLI ships with built-in OAuth credentials. To use your own Trakt API app instead, pass `--client-id` and `--client-secret` flags, or set `TRAKT_CLIENT_ID` and `TRAKT_CLIENT_SECRET` environment variables.

## Commands

All commands support `--json` for machine-readable output.

### search

Search for movies and TV shows.

```bash
trakt-cli search "Severance"
trakt-cli search "Inception" --type movie --json
```

| Flag | Values | Default |
|------|--------|---------|
| `--type` | `movie`, `show`, `movie,show` | `movie,show` |

### history

View watch history.

```bash
trakt-cli history --json
trakt-cli history --type shows --limit 20
trakt-cli history --type movies --json
```

| Flag | Description | Default |
|------|-------------|---------|
| `--type` | `movies` or `shows` | all |
| `--limit` | Items per page | 10 |
| `--page` | Page number | 1 |

### history add

Mark movies or shows as watched. Searches by title and prefers exact matches.

For shows, only episodes that have aired **and** are not already in your watch history are added — catching up on a partially watched show never creates duplicate plays, and a fully caught-up show adds nothing. Season 0 (specials) is never marked watched, matching what `progress` reports. Movies are always added as a new watch.

To deliberately record a rewatch, pass `--rewatch`: it adds a play for every aired episode, including ones you have already seen. Without it, a show you have finished is a no-op.

```bash
trakt-cli history add "Severance" --json
trakt-cli history add "The Sopranos" "The Wire" --json
trakt-cli history add --type movie "The Godfather" --json
trakt-cli history add --watched-at 2025-06-15 "Dark" --json
trakt-cli history add --rewatch "Severance" --json
```

| Flag | Description | Default |
|------|-------------|---------|
| `--type` | `show` or `movie` | `show` |
| `--watched-at` | YYYY-MM-DD or RFC3339 | now |
| `--rewatch` | Add a play for every aired episode, including watched ones | `false` |

JSON output includes a `queries` array with per-query detail for every requested title, show or movie alike (`query`, `type`, `matched`; shows also `new_episodes`, `already_watched_episodes`; a `--rewatch` entry carries `rewatch: true` instead of the episode counters). A title that matches nothing has no `matched` field; a title whose Trakt search failed carries `search_error` instead.

### history remove

Undo a mark-as-watched. Removing a show wipes ALL its episode plays; removing a movie wipes all its watches. There is no per-watch granular removal — Trakt's API removes by trakt id, not by timestamp.

```bash
trakt-cli history remove "Severance" --json
trakt-cli history remove --type movie "The Godfather" --json
```

| Flag | Description | Default |
|------|-------------|---------|
| `--type` | `show` or `movie` | `show` |

### watchlist

View your watchlist.

```bash
trakt-cli watchlist --json
trakt-cli watchlist --type shows --limit 100
trakt-cli watchlist --type movies --json
```

| Flag | Description | Default |
|------|-------------|---------|
| `--type` | `movies` or `shows` | all |
| `--limit` | Items per page | 10 |
| `--page` | Page number | 1 |

### watchlist add

Add movies or shows to your watchlist. Searches by title and prefers exact matches. Items already on the list are reported under `existing_*` counts (not an error).

```bash
trakt-cli watchlist add "Severance" --json
trakt-cli watchlist add "The Bear" "Shrinking" --json
trakt-cli watchlist add --type movie "Oppenheimer" --json
```

| Flag | Description | Default |
|------|-------------|---------|
| `--type` | `show` or `movie` | `show` |

### watchlist remove

Remove movies or shows from your watchlist. Searches by title. Items that were not on the list are silent no-ops (`deleted_*=0`, not an error).

```bash
trakt-cli watchlist remove "Severance" --json
trakt-cli watchlist remove --type movie "Oppenheimer" --json
```

| Flag | Description | Default |
|------|-------------|---------|
| `--type` | `show` or `movie` | `show` |

### calendar

Show upcoming episodes of shows you watch. This is the only read command that returns **forward-looking** data — ideal for proactive "what airs next?" workflows.

```bash
trakt-cli calendar --json
trakt-cli calendar --days 14 --json
trakt-cli calendar --start 2026-04-10 --days 7 --json
trakt-cli calendar --new --days 30 --json  # series premieres only
```

| Flag | Description | Default |
|------|-------------|---------|
| `--days` | Number of days to look ahead | 7 |
| `--start` | Start date (YYYY-MM-DD) | today |
| `--new` | Only series premieres (S01E01 airings) | off |

### progress

Show watch progress for watchlist TV shows.

```bash
trakt-cli progress --json
trakt-cli progress --all --json
```

| Flag | Description | Default |
|------|-------------|---------|
| `--all` | Include not_started and completed | in-progress only |

### sync (opt-in)

Imports what you watched on streaming services into your Trakt history, using your own signed-in browser sessions. **Off by default**: it does nothing until you create a config file, so existing installs are unaffected.

```bash
trakt-cli sync --dry-run          # preview
trakt-cli sync                    # write
trakt-cli sync approve "Title"    # allow a held title
trakt-cli sync deny "Title"       # never sync a title
```

Sources:

| Source | What it reads |
|--------|---------------|
| `upnext` | Apple TV app Up Next via tv.apple.com. Covers apps that report to the TV app (Apple TV+, iTunes, Paramount+, Hulu, HBO Max, Peacock, Prime Video) on all your Apple devices. "Next episode" at E*n* confirms E*n*−1; other earlier episodes are only implied and go to review |
| `netflix` | Viewing Activity for one profile (accounts are shared, so set `profile`) |
| `hbomax` | Per-episode completion for shows seen in Up Next or HBO Max's Continue Watching; verifies the session is on `profile` |
| `paramount` | Keep Watching row; an episode at or past `min_progress` percent counts as watched |

Safety rules, in order: titles on `deny` are dropped; items older than `since` are dropped; items already in your history are skipped. Confirmed watches of shows you already have plays for sync automatically. **Any other show, every movie, and any episode that is only implied (for example skipped-over episodes before an Up Next pointer) is held for review** (listed as "Needs your review", nothing written) until you `approve` it, so someone else using your account never lands in your history.

Config (`~/.config/trakt-cli/sync.yaml`, or `TRAKT_SYNC_CONFIG`):

```yaml
enabled: true
# Prints cookies for a domain. JSON [{name,value}] or a "a=b; c=d" header.
cookie_command: "my-cookie-tool --json --domain {domain}"
since: 30d
deny: []      # other household members' shows
allow: []     # titles you approved (managed by `sync approve`)
sources:
  upnext:    { enabled: true }
  netflix:   { enabled: true, profile: "Your Profile" }
  hbomax:    { enabled: true, profile: "Your Profile" }
  paramount: { enabled: true, min_progress: 80 }
```

| Flag | Description | Default |
|------|-------------|---------|
| `--dry-run` | Preview without writing | off |
| `--source` | Only run these sources | all enabled |
| `--config` | Config path | `~/.config/trakt-cli/sync.yaml` |
| `--max-new-show-episodes` | Skip an approved never-logged show if more episodes than this would be added | 25 |

Exit status is non-zero when any source fails (other sources still sync), which suits a scheduled job. Same-title matches (remakes, reboots) are left unresolved rather than guessed; pin them with `overrides: {"Title": <trakt id>}`.

## AI Agent Plugins

### OpenClaw

The `openclaw/` directory contains a native OpenClaw plugin that registers typed tools: `trakt_search`, `trakt_history`, `trakt_history_add`, `trakt_history_remove`, `trakt_watchlist`, `trakt_watchlist_add`, `trakt_watchlist_remove`, `trakt_calendar`, `trakt_progress`, and `trakt_auth`. The opt-in `trakt_sync` tool is registered only when `syncEnabled` is true.

Install from ClawHub:
```bash
openclaw plugins install trakt-tools
```

Or locally:
```bash
openclaw plugins install -l ./openclaw
```

Configuration (via OpenClaw settings):
- `cliPath`: Path to trakt-cli binary (auto-detected on PATH)
- `clientId`: Trakt API client ID
- `clientSecret`: Trakt API client secret
- `syncEnabled`: register the opt-in `trakt_sync` tool (default `false`; also needs the sync config above)

### Claude Code

The `.claude-plugin/` and `skills/` directories provide a Claude Code plugin with a skill that teaches Claude how to use the CLI.

```bash
claude --plugin-dir /path/to/trakt-plugin
```

## Credits

Originally forked from [angristan/trakt-cli](https://github.com/angristan/trakt-cli). Extended with watchlist, progress, history add commands, `--json` output, and AI agent plugin packaging.

## License

MIT
