# Recommendations with explicit seed connections

`seeded-related` / `trakt_seeded_related` combines the account's positive preference signals with Trakt's related-title endpoint. It does not train a taste model or substitute an invented score for Trakt data.

```sh
trakt-cli seeded-related --type show --source both --min-seed-rating 8 --max-seeds 3 --per-seed 10 --count 5 --json
```

## Contract

- One media type per scan: movie or show. `source` selects favorites, ratings or both; default both. Season/episode ratings are not silently promoted to parent-show preference.
- Favorites qualify independently of rating. Personal ratings qualify at `min_seed_rating` (1-10, default 8). The minimum applies to the **seed's personal rating**, not candidate community ratings. When both sources are read, a favorite's recorded rating remains visible even if below the threshold.
- Strictly read all selected preference records for that type (10,000 per resource maximum), and complete history/watchlist snapshots through the shared exclusion reader (100,000 each maximum). Refuse malformed or duplicate preference entries and incomplete paging. No first-page-only claim of complete preferences.
- Order seeds by favorite status, descending recorded personal rating, then exact string ID. Select at most `max_seeds` (1-10, default 3).
- Read exactly the first related page for each selected seed, at most `per_seed` rows (1-20, default 10). There is no hidden refill or whole-catalog search; at most 200 accepted related rows total. Empty preferences do not trigger trending/popular fallback.
- Exclude watched titles, including parent shows with any episode play, plus eligible seed titles (including eligible seeds beyond the selected seed budget). Watchlist exclusion defaults true; explicit false opts out. Unqualified low-rated titles are not separately excluded unless watched/watchlisted; this is not a dislike/dismissal system.
- Deduplicate candidates while retaining each observed seed connection. Ordering is first appearance in priority-seed/service order, not overlap count or an inferred enjoyment score. Read all selected seed responses before final `count` truncation (1-50, default 5) so early returned candidates can retain later seed connections.
- A full bounded scan requires matching account activity observations across preference, exclusion and related reads. No writes, token refresh/persistence, list creation, history logging, automatic retries or rollback. Activity observations are not a server transaction or an assurance against every race.

`related_to` carries exact seed ID/title, favorite status and any observed rating. The explanation asserts only that Trakt returned the candidate as related to those recorded seeds and it had no play in checked history. It does not assert streaming availability, guaranteed enjoyment, cast/plot similarity, a whole-account ranking or a neural personalization score.

## Result states

`scan_succeeded`, `count_met`, options, selected seeds, `seeds_available`, `seeds_read`, `candidates_observed` and source/exclusion/duplicate counters distinguish a bounded window from the returned top count. Normal stop reasons are `seed_window_complete` and `no_eligible_seeds`; a complete window may still return fewer titles than requested. Source counters count occurrences per seed page, not unique watched titles.

On a request/activity failure, retained items and connections describe only accepted earlier responses and remain unverified partial observations (`scan_succeeded=false`). Items are still count-bounded, but no completion claim or automatic fallback/retry is made. The plugin preserves these structured partial states even on rate limits.

## Verification

Synthetic cases cover overlapping related candidates with both seed connections retained, duplicate removal, watched/watchlist/seed exclusions, favorite/rating source selection, threshold and priority ordering, seed/count bounds, no-seed behavior, malformed preferences, duplicate preferences, rate-limit partial results and activity changes. Closed plugin schemas reject unsupported writes and preserve explicit watchlist opt-out.

Six real dedicated-test-account plugin cases passed: combined show seeds, favorite movies, ratings-only shows, no seeds at a high threshold, a one-seed/two-row budget and watchlist opt-out. The sparse test account's known show rating required an explicitly lowered threshold to exercise a rating seed; defaults were not misrepresented as having populated high-rated show seeds. Raw preference and related responses independently confirmed seed eligibility and every returned connection. These live cases did not produce an overlapping candidate across seeds; multi-seed deduplication/provenance is synthetic-tested.

Original ratings, favorites, history, watchlist objects and credential bytes remained unchanged. No production-account fallback or live mutation. Private receipts remain outside the repository. This feature is local development work, not installed, submitted or released by its implementation commit.
