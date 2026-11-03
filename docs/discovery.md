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
- Empty results are valid. Null and malformed media are refused. Duplicate IDs keep the first service-ranked occurrence.
  Service error bodies are not relayed; tool rate-limit guidance stays structured.
- Filtering defaults off (`watched_filtered:false`). Enable exclusions explicitly
  as below. Genre/year filters and assistant personalization remain future work;
  shortlist previews are supported, but discovery itself never writes a list.

Use an exact result type/ID with the existing preview-first list tool to shortlist,
with explicit authorization before applying. Discovery never performs that step.

## Actual live acceptance

Ten development-plugin calls on the isolated test account passed: four modes for
both media types, plus popular page two for each. Six catalog/related calls
returned three titles each; two recommendations returned empty arrays. Page-two
results were distinct from page one in the observed window. Credential bytes
were unchanged. Populated recommendation decoding is synthetic-tested; a nonempty
real personalized recommendation response remains unverified.


## Optional exclusions and shortlist preview

```sh
trakt-cli discover popular --type movie --exclude-watched --exclude-watchlisted --exclude-ids 55,56 --shortlist my-private-list --json
```

Tool parameters: `exclude_watched`, `exclude_watchlisted`, `exclude_ids` and
`shortlist`. Flags default false; unrequested account reads do not occur.

- Watched reads complete history. Any movie play excludes that movie; any episode
  excludes its parent show, including partially watched shows. It does NOT mean
  the whole show is complete. History covers up to 100 pages/100000 records.
- Watchlist reads are complete and bounded too. A season/episode watchlist entry
  excludes its parent show. Exact movie/show typed identities prevent collisions.
- Missing headers, null arrays, unsupported identities/types, duplicate account
  entry IDs, changed activity markers or exceeded bounds refuse filtering. They
  never silently return partially filtered suggestions. No credential refresh is
  attempted by exclusion reads. Snapshot state can change after the reads.
- `exclude_ids` applies to this call's media type, enabling a caller to carry seen
  IDs between pages without hidden persistence. Up to 10000 canonical IDs.
- Service duplicates keep their first occurrence. Counts distinguish source rows,
  duplicates, watched, watchlisted and explicit IDs. Overlaps count once, in that
  precedence. Ranking of retained entries is unchanged. No page refill occurs;
  page headers still describe the source response, NOT filtered total counts.
- `shortlist` is a list ID/slug. It prepares an offline `ListChangePreview` from
  retained type/IDs only, with applied=false and unresolved owner `me`. Empty
  results produce shortlist_empty, not an empty mutation. Discovery has no apply.
- Review the preview and selected IDs, then use existing `trakt_list_add` with
  explicit apply authorization. That operation separately resolves the active
  account; filtering is not a promise that a future login/list is unchanged.

Actual test-account plugin acceptance excluded seeded watched/watchlisted/seen
movies, proved one watched episode excludes its parent show, and completed a real
preview -> explicit list-add -> readback -> remove workflow. Temporary history and
watchlist entries were removed, and original history/watchlist/list-item objects
and order, ratings, favorites and credential bytes matched baseline. List container
updated_at advanced from those deliberate list writes; byte-identical container
metadata is not claimed. No production account was touched.
