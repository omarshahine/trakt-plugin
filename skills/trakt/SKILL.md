---
name: trakt
description: |
  Search movies/shows, view watch history, check watchlist, track progress, and mark items as watched on Trakt.tv.
  Use when the user asks what they've been watching, what's on their watchlist, what's in progress,
  wants to find a movie or show, mark something as watched, or asks about their Trakt activity.
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
- `--type show` (default) or `--type movie`
- `--watched-at`: RFC3339 or YYYY-MM-DD (defaults to now)
- Accepts multiple titles in one call
- JSON output: `{ "added_episodes": N, "added_movies": N, "not_found_movies": N, "not_found_shows": N }`

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

- **v2.0.0** — Add `progress` command, `--json` flag for all commands (agent-friendly output)
- **v1.1.0** — Add `watchlist` command, `--type` filter for `history`, `history add` with `--watched-at`
- **v1.0.0** — Initial skill (upstream `history` and `search` only)
