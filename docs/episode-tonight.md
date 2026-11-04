# Next episodes for tonight

`episode-tonight` / `trakt_episode_tonight` is a read-only companion to title discovery. It selects one next episode per already-started show, not new shows or a binge playlist.

```sh
trakt-cli episode-tonight --count 3 --max-shows 5 --runtime-max 45 --json
```

## Scope and bounds

- Load a strict, complete episode-history snapshot, bounded to 100,000 events. Incomplete pagination, duplicate event IDs, malformed identities or watch dates refuse the scan. It does not use the older watchlist-based `progress` command's truncated list.
- Order unique shows by latest episode watch timestamp, descending, then exact string show ID. Only shows with recorded regular-episode history qualify by default. Watchlist-only shows are outside scope.
- Check at most `max_shows` shows (1-20, default 5), returning at most `count` suggestions (1-20, default 5). No implicit replenishment beyond that show window. Up to one progress read and one metadata read per checked show, in addition to bounded history and identity/activity reads.
- Require positive started-but-not-caught-up progress and a next episode explicitly marked incomplete in its season progress. A returned episode already present in the history snapshot is withheld, even if progress was reset or contradictory.
- Fetch exact episode metadata independently and verify the Trakt ID against progress. Require `first_aired <= as_of`; missing/invalid dates are unknown and withheld, future dates are withheld. Never skip the provider's blocked next episode to pick a later one.
- `runtime_max` is optional and applies **per episode**, not to the sum of results. When supplied, null/invalid/nonpositive runtime is withheld, never treated as zero. Runtime metadata is not a streaming-availability claim.
- Specials are excluded by default. `include_specials=true` permits season-zero history/candidates using Trakt's progress order with `specials` and `count_specials` enabled. It does not invent a placement for specials relative to regular seasons. A reset progress timestamp is unsupported and withheld.

## Result contract

`scan_succeeded` means the bounded scan finished with unchanged account activity observations, not that every show or the entire catalog was checked. `count_met` describes the returned count. `shows_available`, `shows_checked`, options, skipped-show reasons and `stop_reason` expose the candidate window and limitations.

Normal stops: `target_met`, `show_budget`, `candidate_window_exhausted`. Error stops preserve already-collected items with `scan_succeeded=false`; these are unverified partial observations. Callers must not present them as a fully verified recommendation set. No automatic retry, refresh-token persistence, watch logging, list mutation or rollback occurs. Activity observations are not a server transaction or lock and cannot eliminate all concurrent-write races.

Factual-fit strings explain checked history, explicit incomplete progress, aired metadata and any runtime constraint. They do not claim inferred enjoyment, preferred taste, unobserved viewing history or regional service access. Aired uses a single UTC `as_of` captured at scan start; an episode airing during the scan may be conservatively withheld.

## Verification

Synthetic tests exercise count/show budgets, deterministic recency, runtime/unknown/future dates, specials opt-in/out, already-watched disagreements, caught-up progress, missing completion, reset handling, identity mismatches, malformed history, rate limits with partial output and activity changes. CLI tests reject invalid requests before account/network access; plugin tests check closed read-only schemas and partial-state retention.

Five real development-plugin calls on the dedicated test account verified:

- Three episode suggestions from three shows, independently checked against raw progress and original history.
- Two suggestions within a 45-minute per-episode maximum, with two longer episodes withheld.
- One-minute maximum produced zero matches and explicit runtime skips.
- A one-show budget stopped after exactly one show.
- Specials-enabled queries succeeded; the returned candidates were regular episodes, so positive season-zero selection remains synthetic-tested only.

Original 11 history events and credential bytes remained unchanged. No live writes were used. Future/unknown date cases and failure paths are synthetic tests, not fabricated live observations. Private receipts are not committed. This development feature is not installed, submitted or released by its implementation commit.
