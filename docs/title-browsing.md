# Title, season and episode browsing

```sh
trakt-cli titles details --type movie --id 1 --json
trakt-cli titles details --type show --id 162883 --json
trakt-cli titles seasons --id 162883 --json
trakt-cli titles episodes --id 162883 --season 1 --json
trakt-cli titles episodes --id 1390 --season 0 --json
trakt-cli titles episode --id 162883 --season 1 --episode 1 --json
```

Tools: `trakt_title_details`, `trakt_show_seasons`, `trakt_season_episodes`,
`trakt_episode_details`. All require the configured login and perform one
read-only metadata GET with extended=full. No automatic token refresh/replay,
account mutations, installation or deployment are involved.

## Identity and retained metadata

- `id` is an exact positive Trakt MOVIE/SHOW ID, not title text, TMDB ID,
  history-event ID or episode ID. Details defaults to show and accepts type
  movie/show; the other tools always require a SHOW ID.
- Season is required for episode lists/details. Season 0 is specials, not a
  missing value. Single episode requires an episode number too. Nonnegative
  season/episode numbers up to 10000 are accepted; unavailable items can 404.
- `items` retains service order. Each item has its media type, exact string
  trakt_id where available, season/number where applicable, and raw `media`.
  Season identities are show ID + number even if their Trakt season ID is absent.
- Summaries, runtime, ratings, votes, genres, show status, first_aired, dates and
  other fields remain as returned in raw media. Missing values are not invented.
  Returned ratings are community scores, not the current user's own rating.
- requested season/episode numbers and UTC as_of remain in the result, including
  empty lists. An empty list is valid; null or malformed listing JSON is not.

## Episode dates

`air_status` is computed from first_aired against the single result's UTC as_of:

- aired: valid timestamp at or before as_of;
- upcoming: valid timestamp strictly after as_of;
- unknown: absent, null, empty, zero or invalid timestamp (including date-only).

Unknown is NOT automatically upcoming. These are service broadcast dates, not
watched/unwatched status or regional streaming availability. A show can contain
both future and past episodes; no whole-show completeness is inferred. Seasons
are not assigned an air status from incomplete summary metadata.

## Bounds and evidence

One returned endpoint response per call, no unbounded all-season episode expansion.
Response cap: 8 MiB and 5000 rows. Duplicate IDs/numbers and mismatched title,
season or single-episode identities are refused rather than silently combined.
Service error bodies are not echoed; HTTP failures remain explicit.

Eight actual test-account plugin reads passed: movie/show details, season lists,
thirteen regular episodes, one regular episode, 306 specials and a single special.
Aired status and season-0 round trip were verified live. Upcoming/unknown date
classification, timezone offsets and exact boundaries are synthetic-tested;
positive live upcoming/unknown examples remain unverified. Credentials unchanged;
no writes or production-account fallback.
