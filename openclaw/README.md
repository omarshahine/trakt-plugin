# openclaw-trakt

OpenClaw plugin for [Trakt.tv](https://trakt.tv). Track movies and TV shows, view watch history, manage your watchlist, and check show progress.

## Install

```bash
openclaw plugins install trakt-tools
```

## Prerequisites

The `trakt-cli` Go binary must be installed and on your PATH:

```bash
go install github.com/omarshahine/trakt-plugin@latest
```

Or set `TRAKT_CLI_PATH` environment variable, or configure `cliPath` in plugin settings.

## Configuration

| Setting | Description |
|---------|-------------|
| `cliPath` | Path to trakt-cli binary (auto-detected on PATH) |
| `clientId` | Trakt API client ID (from https://trakt.tv/oauth/applications) |
| `clientSecret` | Trakt API client secret |
| `syncEnabled` | Register the opt-in `trakt_sync` tool (default `false`) |

## Available Tools

| Tool | Description |
|------|-------------|
| `trakt_search` | Search for movies and TV shows |
| `trakt_history` | View watch history |
| `trakt_history_add` | Mark movies/shows as watched |
| `trakt_history_remove_event` | Preview or explicitly remove one dated watch by string history-event ID, with account/type/media/date guards and scoped readback |
| `trakt_backup_restore_ratings` | Offline archive-bound review or explicit restoration of missing dated ratings, with live UUID/conflict checks and value/date readback |
| `trakt_backup_restore_favorites` | Review or explicitly restore missing movie/show favorites membership only; ranks, notes and timestamps excluded |
| `trakt_watchlist` | View watchlist |
| `trakt_progress` | Show watch progress for TV shows |
| `trakt_auth` | Start device authorization, then poll its session after approval |
| `trakt_sync` | Opt-in: import from streaming services (only when `syncEnabled`) |

### Reviewed favorites membership restoration

For favorites membership, `trakt_backup_restore_favorites` uses offline review
and explicit `review_id`/`expect_user`/`apply` gates. It adds only missing
movie/show memberships, respects known account capacity, and verifies the
expected membership union without removing prior favorites. Ranks, notes and
timestamps are not restored. See [favorites restore scope and limits](../docs/favorites-restore.md).

### Reviewed ratings restoration

For ratings restoration, `trakt_backup_restore_ratings` defaults to an offline
preview. Apply requires the exact returned `review_id`, matching `expect_user`
and explicit authorization. Partial/uncertain operations preserve row states;
never retry them blindly. See [ratings restore limits and race semantics](../docs/ratings-restore.md).

### Precise history-event removal

For granular removal, first read the `history_id`, `trakt_id` and exact
`watched_at` returned by `trakt_history`. `trakt_history_remove_event` defaults
to an offline, unverified preview; an authorized apply requires `apply:true`
and matching `expect_user`. It never falls back to all-plays removal. Inspect
structured `operation` output after errors; do not retry uncertain deletions.
See [the complete CLI/tool contract](../docs/history-event-removal.md).

## Authentication

Use `trakt_auth` with `{ "action": "start" }` (or no parameters). Show the
returned `verification_url` and `user_code` immediately. After the user approves,
call it with `{ "action": "poll", "session": "<returned session>" }`, waiting at
least the returned `interval` between pending responses. Stop when the status is
`authenticated`, the session expires, or an error is returned. Keep the session
handle within this authorization flow.

This requires a matching `trakt-cli` with the `auth --session` option. The plugin
checks that capability before starting authentication. On POSIX systems, update
an older CLI or run `trakt-cli auth` locally for interactive setup. On Windows,
complete initial authentication with `trakt-cli auth` locally; nonblocking private
sessions are currently supported on POSIX systems. Configured client credentials
are passed in a separate child environment rather than command-line arguments.

## License

MIT

### Ratings and favorites

`trakt_ratings` and `trakt_favorites` read raw paginated records with optional
`type`, `page` and `limit`. Ratings support movie/show/season/episode; favorites
support movie/show; read type defaults `all`. `trakt_rating_set` and
`trakt_rating_remove` set/change/remove ratings; `trakt_favorite_add` and
`trakt_favorite_remove` manage favorites. Mutation `type` and exact Trakt media
`ids` (strings) are required. Ratings set also requires integer `rating` 1-10;
optional `rated_at` is an ISO8601 timestamp, normalized to UTC, omitted uses now.

All write tools default to offline preview without authentication/network.
`apply: true` requires `expect_user` to match the token's freshly resolved account
before any write. This guard does not select credentials: only the existing
single `~/.trakt.yaml` login is supported. Named profiles/two simultaneous logins
are not implemented. Isolate test credentials before live test-account use.

Obtain user authorization before applying. Mutations disable automatic replay;
inspect partial/not_found, limit and uncertain results before a manual retry.
Structured partial outcomes are retained by the plugin. No history/list changes,
favorite notes or favorite reordering are added. Tests use synthetic accounts;
live service acceptance is not yet verified.

### Custom-list management

The tools `trakt_list_create`, `trakt_list_add`, `trakt_list_remove`,
`trakt_list_notes` and `trakt_list_reorder` default to offline previews, requiring
no login. Only explicit `apply: true` executes a write against the token's freshly
resolved account. Obtain user authorization before applying. New lists default
private. Membership accepts Trakt media IDs as strings; notes and ordering require
list-entry IDs from `trakt_list_items`. Only movie/show/season/episode membership
is supported. Empty notes clear them; notes are VIP-only and capped at 500 code
points. Non-VIP owners are refused before a notes write; HTTP 420 reports list/item
limits without retries.

Reorder takes the complete ordered set of current list-entry IDs, checks all
pages and stable list metadata, and refuses missing/stale/duplicate IDs. This is
not a transactional lock. Display sorting is unchanged. Partial outcomes preserve
available counts and exit nonzero; uncertain writes must be inspected, never
automatically replayed. No whole-list delete, history edits, or collaboration-owner
selection is added. Names and notes in previews/results are private account data.
Synthetic tests verify contracts; live mutation acceptance is not yet tested.

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
There is no general full-account restore; reviewed ratings and favorites-membership
executors have separate, limited scopes. `complete` means the declared API scope passed all
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
OpenClaw also exposes `trakt_backup_plan_restore` with `source`, `current` and
optional `import_json`. It works offline without authentication, validates both
archives, requires matching accounts and previews missing watches/watchlist/ratings.
Current means a supplied snapshot: run `trakt_backup` first to refresh it. Conflicts,
ambiguous repeat watches and missing coverage are withheld from import JSON.
Custom-list gaps are reported but cannot be targeted by Trakt's website JSON format.
Other categories, notes/ranks and historical supplemental fields are not restored.
The optional NEW JSON file follows Trakt's website import guidelines, using string
`trakt_id` values and one action per row. It is never uploaded or imported by this
tool; actual website import acceptance remains untested. Reports/files contain
private IDs, dates and ratings. No account write or delete functionality is added.
The development CLI and plugin must both include this feature. Large histories can
take several minutes; use the CLI directly if the plugin timeout is insufficient.

### Collection restore planning

`trakt_backup_plan_collection` reads local source/current ZIPs without login or
network. It distinguishes missing movies/episodes from collected-date/technical
metadata conflicts. No apply mode exists; current-only entries are not deletions.
See [scope and observed fidelity](../docs/collection-restore-planner.md).

Missing-only collection restore executor: `trakt_backup_restore_collections` — [contract and live acceptance](../docs/collection-restore.md).

### Discovery

`trakt_discover_trending`, `trakt_discover_popular`,
`trakt_discover_recommendations` and `trakt_discover_related` read movies/shows
with full returned metadata and exact string media IDs. Related requires `id`;
recommendations belong to the configured login and may be empty. No watched
filter or list writes implied. See [scope and acceptance](../docs/discovery.md).
