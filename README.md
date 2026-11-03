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

### Custom lists (read-only)

```bash
trakt-cli lists --limit 100 --page 1 --json
trakt-cli lists items --list 12345 --limit 100 --page 1 --json
trakt-cli lists items --list weekend-films --json
```

Uses your existing login and reads your own lists, including private lists your
account can access. Use a Trakt ID or slug from `lists`, not a display name.
`--page` must be positive; `--limit` is 1-1000 (default 10).

JSON output is `{ "items": [...], "page", "limit", "page_count", "item_count" }`.
Records preserve **all fields returned by the API**, including list metadata,
item IDs, ranks, timestamps, notes and mixed movie/show/season/episode/person
records. Fields present in the website export but omitted by the API cannot be
reconstructed by these commands. Pagination values come from response headers;
missing headers are empty strings, not an assertion that the data is complete.
Follow `page_count` where provided. No automatic all-pages export is performed.
These commands never create, edit, reorder or delete lists and are not a full
account backup or restore feature. API failures exit nonzero.

OpenClaw exposes `trakt_lists` and `trakt_list_items` with the same pagination
parameters (`trakt_list_items` also requires `list`). Both the plugin and the
companion CLI must include this feature; an older CLI will reject `lists`.

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
| `paramount` | Keep Watching row; an episode at or past `min_progress` percent counts as watched; verifies the session is on `profile` |

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
  paramount: { enabled: true, profile: "Your Profile", min_progress: 80 }
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

### Scoped raw backup (read-only Trakt access)

```bash
trakt-cli backup --output ./trakt-backup.zip --json
```

Creates a **new** ZIP with every page of history, watchlist, custom-list metadata,
each owned custom list's contents, movie/show/season/episode ratings,
movie/show/episode collections, and mixed movie/show favorites.
Also includes playback progress, watched movie/show summaries, hidden calendar/
progress/reset/recommendation items, comments including replies, all eight
website note categories, follow requests/followers/following/friends, liked
comments/lists, saved filters, and collaborated-list metadata.
Collections request `extended=metadata` and retain returned collection timestamps,
technical metadata, and nested season/episode data. Reuses the current login; does not change
Trakt or include OAuth credentials. Existing output files are never overwritten.
Keep the ZIP private. It contains your account data.

`manifest.json` records the explicit scope/exclusions, start/end times, per-resource
expected/actual counts, page counts, per-file SHA-256 checksums and `complete`.
Records retain API fields, item ordering and individual history event IDs (including
rewatches). Only list inventory and seen IDs, not all history pages, are held in memory.

Completeness requires valid stable page counts and page sizes, exact collected
counts, unique record IDs, matching list metadata counts, and an unchanged
`/sync/last_activities` marker. History totals on earlier pages can be stale:
the collected history count must match the terminal page's reported total.
Trakt does not provide a transactional snapshot: an unchanged activity marker is a
consistency check, not an absolute snapshot guarantee. A changed marker fails the export.

Defaults: `--limit 1000`, `--delay-ms 1000`, `--max-pages 10000` per resource.
Backup reads request HTTP cache revalidation; this does not guarantee the service
honors it. Missing pagination, an API error, changed page counts or page sizes,
unreconciled item counts, duplicate IDs, or the page cap
stops the export. No automatic retry is made after a rate limit. Failed exports
exit nonzero and retain available pages with `complete: false` and errors in the
manifest when storage permits. Do not treat a partial ZIP as a complete backup.

Ratings and collections are identified by the corresponding media's Trakt ID,
not its parent show's ID. Their pagination totals remain strict; history's
terminal-count reconciliation does not silently relax other categories.

Raw account settings (including browsing/sharing), the full/VIP profile, statistics
and the initial activity snapshot are stored as JSON objects under `user/`.
Other resources remain raw paginated arrays. Follow requests are the sole
explicitly unpaginated array endpoint; unexpected pagination there fails safely.
Missing headers on other array resources still fail rather than imply completeness.

This targets the categories in a website account export, but does **not** promise
byte-identical files, future website categories, or fields the API does not return.
Only collaborated-list metadata is included, not contents owned by other users.
There is no restore support. `complete` means the declared API scope passed all
checks, not a guarantee of website-field parity or a transactional snapshot.
For example, third-party API responses can omit the account email, date of birth,
and numeric user Trakt ID even when the website export contains them. These fields
are not fabricated or recovered from an older export. Likewise, playback entries
no longer returned by the live API are not resurrected from earlier snapshots.

#### Optional website-only account fields

```bash
trakt-cli backup import-supplement --website-export ./official.zip --output ./account-supplement.json
trakt-cli backup --output ./supplemented.zip --website-supplement ./account-supplement.json --json
```

Import saves only the account email, date of birth and numeric user ID, with
account slug/UUID and the official ZIP's SHA-256. The import source is unchanged;
existing supplement/output files are never overwritten. The supplement contains
private account data and should stay private. Import output does not print its values.

Opt-in exports bind the supplement to the live settings UUID and account slug.
The profile must match that slug and any UUID/numeric ID it returns. Only absent
keys are filled: an explicit null or value from the current API is retained.
The original API settings/profile remain under `raw-api/user/`; supplemented
versions are under `user/`, with checksums for both.

The manifest's `mode` is `api_only` by default or `website_supplemented` when
enabled. Its `supplement` section records the source checksum, import timestamp,
applied field paths and raw API paths, and marks values as historical and not
revalidated. The website snapshot time is explicitly unknown, not inferred from
the import time. Import a new supplement after account details change; the
exporter does not fetch or refresh these saved values automatically.

This closes known missing account-field paths, not a promise that a newer backup
equals an older website snapshot or that future website fields are covered.

#### Restore planning and website import JSON (preview only)

```bash
# Offline: supply a freshly created current-account backup.
trakt-cli backup plan-restore --source ./desired.zip --current ./current.zip --json
# Or fetch a fresh API snapshot to a NEW ZIP, then plan.
trakt-cli backup plan-restore --source ./desired.zip --live-current ./new-current.zip --import-json ./review-import.json --json
```

Both archives use the comparison reader's integrity checks; partial native
backups are refused. Matching account identities are required. Website-layout
ZIPs still have unknown completeness. The current archive is a snapshot, not a
lock: refresh it immediately before reviewing any eventual manual import.

Plans contain additions only: missing movie/episode watches, watchlist entries,
and ratings. Watches are matched by media ID plus normalized timestamp, rather
than watch-event ID alone; repeated watches at different times are retained.
Ambiguous identical-time repeats, unknown-date ambiguity, invalid dates/ratings,
and changed ratings are conflicts, not automatic changes. No timestamps are
fabricated. Explicit `watched_at: "unknown"` is preserved; a missing/null watch
timestamp is not silently treated as unknown. Missing coverage is not deletion.
Custom-list gaps are reported separately: the website importer has no target-list
field. Other categories are explicitly unsupported; notes, rank, supplemental
account fields and history-event IDs are not restored.

Optional `--import-json` creates a **new** local array containing conflict-free
candidate rows using `trakt_id` (string), `type`, and the appropriate `watched_at`,
`watchlisted_at`, or `rating`/`rated_at` fields from Trakt's JSON guidelines. It
does **not** upload or import this file. Each row contains one action; no unrelated
actions are combined. Media without a usable Trakt ID are withheld. Missing rating
timestamps are omitted, as allowed by the format. Actual website import acceptance
and duplicate-watch behavior have not been live-tested; review before use.
Plans and import files contain private IDs/dates/ratings. Input ZIPs are unchanged.

OpenClaw exposes `trakt_backup_plan_restore` with `source`, `current`, and optional
`import_json`. This plugin tool is offline and needs no Trakt authentication;
use `trakt_backup` first for fresh account state. No restore API calls or deletions
are implemented. No backup or report should be committed to the repository.

#### Read-only archive comparison

```bash
trakt-cli backup compare --before ./earlier.zip --after ./later.zip --json
```

This command only reads the two local ZIPs. It does not load Trakt credentials,
contact the API, modify archives, or restore anything. OpenClaw exposes
`trakt_backup_compare` with `before`, `after` and optional `sample_limit`.

Native backups must have a supported, complete manifest. Comparison verifies
ZIP reads/CRC, each listed SHA-256 and count, resource totals, contiguous page
names, unique record identities, and absence of unlisted files. Partial or
corrupt native backups are refused. The flat filename layout of official
website ZIPs is also recognized; CRC/JSON/identity checks cannot establish their
account-wide completeness or authenticity, so completeness is reported unknown.
Unrecognized files and detectable intermediate page gaps fail explicitly.

Known account identities must not conflict. When the two archives lack enough
identity information to match accounts, the report says `unverified`.
Resources covered on only one side are coverage differences, not inferred
additions or deletions. Shared resources report added/removed IDs and changed
records, separating media-metadata-only changes, record-data changes and account
snapshots. These are archive differences, not proof of account actions.

JSON object-key order and whitespace do not cause changes; array order remains
significant. IDs retain integer precision. Samples show only IDs and changed
field paths, never before/after values. `--sample-limit` defaults to 10 per change
type/resource (0 disables samples; maximum 100). Each changed sample includes at
most 20 field paths. Raw API snapshots are validated but excluded from logical
change totals. Historical supplement provenance and freshness-review warnings
remain visible, including when both archives reuse the same saved source.

Comparison refuses archives with over 10,000 ZIP members, an expanded member
over 16 MiB or a total expanded size over 512 MiB.
OpenClaw exposes `trakt_backup` with `output`, `limit`, `delay_ms` and `max_pages`.
The development CLI and plugin must both include this feature. Large histories can
take several minutes; use the CLI directly if the plugin timeout is insufficient.
