# Bounded watch-tonight shortlists

```sh
trakt-cli shortlist --type movie --count 5 --limit 10 --max-pages 5 --genres drama,thriller --runtime-max 120 --min-rating 7 --min-votes 100 --json
trakt-cli shortlist --mode related --id 162883 --type show --count 5 --json
trakt-cli shortlist --exclude-watched=false --exclude-watchlisted=false --json
```

Tool: `trakt_watch_tonight`. Read-only; optional shortlist target prepares an
unapplied list-add preview, never writes or asks the API to dismiss suggestions.
All existing discovery criteria and explicit seen-ID exclusions are reused.

## Bounds and defaults

- Source mode popular, type show, count 5, catalog limit 20 per page, max_pages 5,
  starting page 1. Count 1-50, limit 1-100, max_pages 1-20; at most 2000 catalog rows
  can be accepted under the largest configuration. Starting page bounded at 1000000.
- Watched/watchlist exclusions default TRUE here, unlike raw discovery. Explicit
  false works in CLI and tools. Any episode play excludes its parent show, not
  merely completed shows. Exclusions and genre catalogs are loaded once per scan.
- Recommendations intentionally reads ONE response even if max_pages is larger;
  effective_page_budget is 1. It never fabricates unsupported recommendation pages.
- Metadata filters stay local to observed source pages; no undocumented server
  query parameters. First-seen service order is preserved. Shared typed source
  IDs deduplicate across pages, including previously filtered-out candidates.
- The response body must not exceed requested per-page limit. Pagination headers
  must be structurally consistent to advance; missing headers stop rather than
  authorize blind loops. Dynamic public ranking is not a transactional snapshot.

## Results and explanations

Every item retains exact string ID and full returned media plus requested_page
and factual_fit. Explanations name source, matched genre/year/runtime/rating/vote
facts where requested, and absence from checked history/watchlist snapshots.
They do NOT infer private taste, promise enjoyment, establish regional streaming
availability or claim that unlogged viewing never happened. Show runtime means
per-episode metadata, not a binge duration; year means series premiere.

scan_succeeded reports whether this bounded scan ended normally. count_met is
separate. catalog_complete is always false: a budget-limited or empty result
never claims no matching titles exist elsewhere. Starting/last requested pages,
accepted pages/rows, duplicate/exclusion/unknown counters and matches_observed
remain explicit. The last page can contain more eligible matches than requested;
only the requested count is selected. Counters cover successfully accepted pages.

Normal stop_reason values: target_met, page_budget, end_of_reported_pages,
pagination_unavailable, recommendations_single_response. The reported end is for
that service source, not a universal matching catalog. A later request, malformed
pagination or changed/unverified account activity returns failure with prior
accepted items retained, scan_succeeded=false and no list-add preview. Account
activity is checked around the full scan, not reread history on every page.

No token-refresh replay, automatic retry or rollback. Rate-limit errors preserve
partial items plus structured wait guidance. A successful but short bounded scan
may still prepare a preview for its smaller set; count_met makes that explicit.
The preview has unresolved owner me; applying it later uses the existing separate
list tool with explicit authorization and fresh ownership checks.

## Actual isolated-account acceptance

Eight real plugin cases passed: movie 5 across 5 pages; show 3 across 2; explicit
budget/empty results; one-response empty recommendations; exclusion opt-out;
related 2 across 1; trending 4 across 2. Metadata predicates, unique IDs, count sums,
explanations and unapplied preview IDs were independently asserted. Credentials
unchanged, no account writes. Populated recommendations and injected pagination/
network/activity failures remain synthetic-tested rather than invented live cases.
