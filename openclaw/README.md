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
