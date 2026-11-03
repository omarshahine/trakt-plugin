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
| `trakt_watchlist` | View watchlist |
| `trakt_progress` | Show watch progress for TV shows |
| `trakt_auth` | Set up Trakt.tv authentication |
| `trakt_sync` | Opt-in: import from streaming services (only when `syncEnabled`) |

## License

MIT

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
