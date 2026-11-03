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
  page support. Each call reads one catalog response; genre/account lookups are additional reads.
- Empty results are valid. Null and malformed media are refused. Duplicate IDs keep the first service-ranked occurrence.
  Service error bodies are not relayed; tool rate-limit guidance stays structured.
- Filtering defaults off (`watched_filtered:false`). Enable exclusions explicitly
  as below. Metadata filters are supported below; assistant personalization remains future work;
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


## Page-local metadata criteria

```sh
trakt-cli genres --type movie --json
trakt-cli discover popular --type movie --genres drama,thriller --year-min 2000 --year-max 2026 --runtime-min 20 --runtime-max 150 --min-rating 6 --min-votes 100 --json
```

Tool parameters are `genres`, `year_min`, `year_max`, `runtime_min`,
`runtime_max`, `min_rating`, `min_votes`. `trakt_genres` returns current genre
names/slugs; selected slugs are validated against that live catalog. CLI genre
slugs are trimmed/lowercased/deduplicated. Unknown genres explicitly refuse.
Catalog lookup is bounded to 256 KiB/200 rows and rejects malformed/null data.

These criteria apply LOCALLY to the single returned page/response for every
mode. No undocumented server query parameter is sent, no automatic page refill
occurs, and an empty page does not mean Trakt has no matching title elsewhere.
`metadata_filter_scope:returned_page_only` is explicit. Source pagination totals
remain unfiltered. All selected criteria AND; genre choices OR. Ranking of
retained titles is unchanged. Shortlists contain only the retained matches.

- Year is movie catalog year or SHOW PREMIERE year, not its latest season year.
  Bounds are inclusive, client range 1800-9999.
- Runtime is returned minutes: movie length or show per-episode metadata, NOT
  total series/binge length. Inclusive input bounds 1-10000 minutes; a known
  longer runtime remains a known value, not an invented missing value.
- Rating is the community 0-10 score, not personal taste or a user's own rating.
  Missing/invalid/zero rating is unrated and excluded when rating is required,
  even if the supplied minimum is zero.
- Votes is a nonnegative integer. Explicit min_votes=0 accepts a known zero;
  missing/null/malformed votes are still unknown and excluded.
- Missing/null/invalid active fields, empty/unknown genre assignment and
  zero/nonpositive runtime never silently pass. Unrequested fields do not
  exclude a title. Invalid ranges/nonfinite scores refuse before API calls.

`excluded_metadata` counts rejected unique candidates after earlier watched/
watchlist/seen-ID exclusions. It equals `unknown_metadata + metadata_mismatches`.
A title with any unknown active field goes in unknown_metadata, even if another
known criterion also mismatched. `unknown_metadata_fields` counts each missing
active field per rejected title. It can exceed the number of rejected titles.
No hidden preferences are stored.

Real test-account plugin audit retained7/20 movies and19/20 shows using genre,
year, runtime, rating and vote criteria, matching independently filtered original
page IDs/order. No-match and unknown-genre refusal passed live. All required
fields in that live sample were known; missing/unrated metadata and boundaries
remain synthetic-tested rather than fabricated live cases. No account writes.
