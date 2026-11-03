# Movie and show discovery

```sh
trakt-cli discover trending --type show --limit 10 --json
trakt-cli discover popular --type movie --page 2 --limit 10 --json
trakt-cli discover related --type show --id 162883 --limit 10 --json
trakt-cli discover recommendations --type show --limit 10 --json
```

Tools: `trakt_discover_trending`, `trakt_discover_popular`,
`trakt_discover_related`, `trakt_discover_recommendations`.

Uses the configured Trakt login for every read. Recommendations belong to that
login, not an independently inferred user profile. Every operation is a read-only
GET; no dismissal, list/history changes or automatic credential-refresh replay.

- Type movie/show, default show. Requested limit 1-100, default 10.
- Related requires an exact positive Trakt MEDIA ID, not a title/event/TMDB ID.
  Other modes reject an ID. Media IDs are returned as strings without float loss.
- Trending/popular/related accept an explicit positive page. Recommendations is
  intentionally limit-only: CLI page other than 1 is rejected and the tool has
  no page parameter, even if the service returns pagination headers.
- Retains service order, type, title, nullable year, trending watchers when
  present, and raw extended `media` (overview, IDs, genres, ratings/runtime when
  returned). Unavailable fields are not fabricated. Trakt rating is not the
  current user's own rating.
- Pagination headers are retained. `pagination_available` means the four headers
  exist, not proof of full catalog coverage, stable ranking or recommendation
  page support. Each call reads one response only.
- Empty results are valid. Null, malformed media and duplicate IDs are refused.
  Service error bodies are not relayed; tool rate-limit guidance stays structured.
- `watched_filtered:false` is explicit. Watched/watchlisted titles are NOT
  excluded. Genre/year filters, assistant personalization and shortlist writes
  remain separate future work.

Use an exact result type/ID with the existing preview-first list tool to shortlist,
with explicit authorization before applying. Discovery never performs that step.

## Actual live acceptance

Ten development-plugin calls on the isolated test account passed: four modes for
both media types, plus popular page two for each. Six catalog/related calls
returned three titles each; two recommendations returned empty arrays. Page-two
results were distinct from page one in the observed window. Credential bytes
were unchanged. Populated recommendation decoding is synthetic-tested; a nonempty
real personalized recommendation response remains unverified.
