---
name: trakt
description: |
  Search movies/shows, view watch history, check and update the watchlist, track progress, view the upcoming-episodes calendar, and mark items as watched (or undo) on Trakt.tv.
  Use when the user asks what they've been watching, what's on their watchlist, what's in progress, what airs next,
  wants to add/remove a movie or show, mark something as watched, undo a watch, or asks about their Trakt activity.
---

# Trakt Skill

View watch history, watchlist, progress, search, and mark items as watched on Trakt.tv.

All commands support `--json` for machine-readable output. **Always use `--json` for data processing.**

## Commands

### Progress (In-Progress Shows)

Shows which watchlist shows are started but not finished, not started, or completed.

```bash
trakt-cli progress --json
trakt-cli progress --all --json
```

- Default: shows only in-progress items + summary counts
- `--all`: includes not_started and completed lists
- JSON output: `{ "in_progress": [...], "summary": { "in_progress": N, "not_started": N, "completed": N } }`
- Each item: `{ "title", "year", "trakt_id", "aired", "watched", "remaining", "percent", "status", "next_episode" }`

### Watchlist

```bash
trakt-cli watchlist --json
trakt-cli watchlist --type shows --limit 100 --json
trakt-cli watchlist --type movies --json
```

- JSON output: `{ "items": [{ "type", "title", "year", "trakt_id", "added_at" }], "page", "page_count", "item_count" }`
- `--type`: filter by `movies` or `shows`
- `--limit`: items per page (default 10)
- `--page`: page number

### Add to Watchlist

```bash
trakt-cli watchlist add "Severance" --json
trakt-cli watchlist add "The Bear" "Shrinking" --json
trakt-cli watchlist add --type movie "Oppenheimer" --json
```

- Searches by name, prefers exact title matches
- `--type show` (default) or `--type movie`
- Accepts multiple titles in one call
- Duplicates are NOT an error — items already on the list come back under `existing_*` counts
- JSON output: `{ "added_movies", "added_shows", "existing_movies", "existing_shows", "not_found_movies", "not_found_shows" }`

### Remove from Watchlist

```bash
trakt-cli watchlist remove "Severance" --json
trakt-cli watchlist remove --type movie "Oppenheimer" --json
```

- Symmetric inverse of `watchlist add`
- Items not on the list are silent no-ops (`deleted_*=0`, not an error)
- JSON output: `{ "deleted_movies", "deleted_shows", "not_found_movies", "not_found_shows" }`

### Calendar (Upcoming Episodes)

```bash
trakt-cli calendar --json                    # next 7 days starting today
trakt-cli calendar --days 14 --json          # next 14 days
trakt-cli calendar --start 2026-04-10 --days 7 --json
trakt-cli calendar --new --days 30 --json    # series premieres only
```

- **Only forward-looking trakt command** — use it whenever the user asks "when does X come back?" or "what's new this week?"
- Covers shows already in the user's watchlist/history (Trakt derives "my shows" from these)
- JSON output: `{ "items": [{ "first_aired", "show_title", "show_year", "show_trakt_id", "season", "episode", "title" }], "count" }`
- One row per episode airing — a show with 3 new episodes in the window produces 3 rows
- `--new` filters to series premieres only (S01E01 airings) — useful for "what's launching soon?"

### Watch History

```bash
trakt-cli history --json
trakt-cli history --type shows --limit 20 --json
trakt-cli history --type movies --json
```

- JSON output: `{ "items": [{ "type", "title", "year", "watched_at", "season", "episode", "show_title" }], ... }`
- Episodes include `show_title`, `season`, `episode` fields

### Mark as Watched

```bash
trakt-cli history add "Pluribus" --json
trakt-cli history add "The Sopranos" "The Wire" --json
trakt-cli history add --type movie "The Godfather" --json
trakt-cli history add --watched-at 2025-06-15 "Dark" --json
```

- Searches by name, prefers exact title matches
- For shows, only episodes that have aired **and** are not yet in your
  history are added: catching up on a partially watched show never creates
  duplicate plays, and a fully caught-up show adds nothing
- Repeated titles (or aliases resolving to the same show) are handled once
  per call — later matches add nothing and come back with a
  `duplicate_of` field in `queries` (text output: `duplicate of "<query>"`)
- Every requested title gets a `queries` entry in JSON output, show or
  movie alike (`query`, `type`): matched titles carry `matched` (shows
  also `new_episodes` / `already_watched_episodes`), titles that match
  nothing have no `matched` field, and a title whose Trakt search failed
  carries `search_error` — so a failed lookup is distinguishable from
  "not found"
- `--type show` (default) or `--type movie`
- `--watched-at`: RFC3339 or YYYY-MM-DD (defaults to now)
- Accepts multiple titles in one call
- JSON output: `{ "added_episodes": N, "added_movies": N, "not_found_movies": N, "not_found_shows": N, "not_found_seasons": N, "not_found_episodes": N, "queries": [{ "query", "type", "matched", "new_episodes", "already_watched_episodes", "duplicate_of", "search_error" }] }`
- A show whose pending-episode lookup fails is skipped and reported in an
  additive `skipped_shows` array; if every lookup fails, nothing is written
  and the command exits 1 with `{"error": "pending episode lookup failed", "skipped_shows": [...]}`
- On a Trakt rate limit (429) the command stops immediately, syncs nothing,
  and exits 1 with `{"error": ..., "retry_after": N}` (marker also on
  stderr) — wait out `retry_after` before calling any trakt tool again

### Undo Mark as Watched

```bash
trakt-cli history remove "Severance" --json
trakt-cli history remove --type movie "The Godfather" --json
```

- Removes ALL plays of the matched item — there is no per-watch granular removal
- Removing a show wipes all its episode plays; removing a movie wipes all its watches
- `--type show` (default) or `--type movie`
- JSON output: `{ "deleted_movies", "deleted_episodes", "not_found_movies", "not_found_shows" }`
- **Warning:** destructive. Confirm with the user before calling on a show with heavy watch history

### Search

```bash
trakt-cli search "Shogun" --json
trakt-cli search "Inception" --type movie --json
```

- JSON output: `{ "items": [{ "type", "title", "year", "trakt_id", "imdb" }] }`
- `--type`: `movie`, `show`, or `movie,show` (default)

## Notes

- Always use `--json` flag — raw table output is for human use only
- No shell constructs (pipes, redirects, chaining)
- Auth stored in `~/.trakt.yaml` (OAuth device flow)

## Changelog

- **v2.2.0** — Add `watchlist remove`, `history remove`, and `calendar` commands (`trakt_watchlist_remove`, `trakt_history_remove`, `trakt_calendar` tools). Calendar unlocks forward-looking "what airs next?" workflows.
- **v2.1.0** — Add `watchlist add` command (`trakt_watchlist_add` tool) for adding movies/shows to the Trakt watchlist
- **v2.0.0** — Add `progress` command, `--json` flag for all commands (agent-friendly output)
- **v1.1.0** — Add `watchlist` command, `--type` filter for `history`, `history add` with `--watched-at`
- **v1.0.0** — Initial skill (upstream `history` and `search` only)
