# Live verification and remaining work

Last verified: 2026-11-03. These results cover the local development stack,
including feature branches not yet merged upstream. They do not describe the
currently released plugin. Testing used a separate non-VIP account; production
credentials were inactive. Private fixtures, archives and account details are
not included in this repository.

## Verified behavior

- 42 real plugin calls: 36 successful calls, five expected refusals and one
  inconsistent-pagination export correctly stopped. A successful export retry
  is included in the successful count.
- A small sample spanned 1990 through 2026 and included movies, regular television,
  limited-series episodes, and movie/show/season/episode list items and ratings.
- Private-list creation, membership changes and full-list reordering worked.
- Rating reads, changes and removals, and favorite additions/removals worked.
  Wrong-account guards and non-VIP note rejection refused writes as intended.
- Deliberately removed history, rating and watchlist records were detected by the
  read-only restore planner. The missing custom-list item was reported separately.
- A private test harness translated generated import rows into sync API requests;
  this is NOT a product restore executor or a test of the website importer.
- Final readback matched the seeded history media IDs/dates, rating values/dates,
  list membership/order, favorites membership and collection sample. Re-created
  record IDs and watchlist timestamps need not match.
- The final planner found no remaining supported restore candidates or conflicts.
- Four complete archives passed independent checksums, counts and ZIP checks.
  Incomplete archives and cross-account comparisons/plans were refused.
- Supplement account binding refused personal-account values on the test account.
  The original source archive remained unchanged.
- Go tests, vet, CI-matching golangci-lint, plugin tests, TypeScript and plugin
  inspection passed on the integrated development stack.

## Known issues and follow-up

### 1. Repeated-watch restoration: account setting identified; safeguard implemented

Submitting two different dates for the same movie did not retain two events in
the tested account. Readback returned one event despite the bulk added counts.
A subsequent request returned an updated count, reused the history ID and changed
its date. Both history read routes agreed on one play.

Follow-up on 2026-11-03 identified `browsing.watch_only_once: true` in the
extended settings of the isolated test account. A controlled second-date request
returned `added.movies: 0`, `updated.movies: 1`; readback retained the same
history ID and replaced its date. Restoring the original date reproduced the
original history record exactly. No account preference or production data changed.

The local planner now reports `history_policy` and withholds repeat-watch rows
when that setting is true or unverified. Single missing watches, ratings and
watchlist additions remain independently eligible. Source archive settings do
not establish current policy. Missing/null/malformed policy is unknown, not false.
CLI history-add output retains API `updated` counts rather than discarding them.

Still unverified: positive repeated-watch preservation with the setting disabled,
and official website import acceptance. The setting must be checked in the
official Trakt interface before another live positive test; third-party settings
writes are not assumed supported. This is not evidence of a non-VIP limitation.
Any future executor must verify multiplicity and dates by readback, not added
counts alone. Never replay uncertain mutations automatically.

### 2. Historical watchlist timestamps: not preserved by tested sync requests

Membership was restored, but none of four historical dates supplied as `listed_at`
were retained. The server assigned current timestamps. Re-adding an item also
changed its timestamp relative to the seeded snapshot. The public sync request
schema does not advertise this date field.

TODO: verify a supported date-preserving method. Trakt's website JSON guidelines
describe `watchlisted_at`, but actual website import acceptance remains untested.
Do not claim timestamp fidelity for the tested sync API path.

### 3. Pagination immediately after mutations: inconsistent service responses

A history deletion produced conflicting page counts/totals across pages. The
exporter correctly retained an incomplete ZIP and failed. A subsequent run with
a different page size completed with stable observations. Cache behavior is a
possibility, not an established cause.

TODO: investigate bounded read-only retry/backoff or snapshot strategies while
preserving completeness checks. Never apply this as automatic mutation replay.

### 4. Empty-account statistics: fixed locally

The statistics endpoint returned HTTP 204 for the empty account. The exporter now
records JSON `null` with an explicit `http_status: 204` manifest marker rather than
inventing zero totals. The native comparison reader requires that marker for a
null statistics snapshot. Other snapshots still require their normal shape.
Regression tests cover acceptance and refusal without the marker.

### 5. Remaining acceptance coverage

- Positive VIP-only item-note writes remain synthetic-tested only. The live
  non-VIP refusal passed; no production-account fallback was used.
- Uploading generated JSON through the official website importer remains untested.
- Empty comments, hidden/playback, social, note and filter resources were read;
  populated write/read fidelity for all such categories was not established on
  this account. No public comments or social actions were seeded.
- Collaborated-list metadata does not imply coverage of other owners' contents.
- Official VIP archive retrieval and a general multi-category restore executor
  are not implemented. A ratings-only executor is now implemented locally; see
  [ratings-restore.md](ratings-restore.md). Live tests used actual native
  source/current/restored backups: a missing movie and episode rating were
  restored with their original dates, a conflict introduced after preview refused the entire
  reviewed batch, and an explicit repeat apply was a verified no-op. All four
  rating categories matched the source archive after restoration. Dedicated
  test ratings were removed afterward; the original six ratings and 11 history
  records were unchanged. Original ZIP bytes and credentials were unchanged.
- Granular history-event removal is implemented locally in the CLI and plugin.
  Live tests removed dedicated movie/episode events, refused a wrong account and
  wrong date, and refused a later apply of the already removed episode. Final
  readback matched all 11 original account-history records exactly. Credentials
  remained unchanged. Same-media repeat preservation is synthetic-tested; the
  live test account still has multiple plays disabled.
- The first deletion returned an uncertain result because Trakt represented an
  empty media history with `page_count:0`. Independent readback resolved that
  outcome without replaying the deletion. The reader now accepts only the exact
  consistent empty-first-page case; subsequent dedicated movie and episode
  deletions passed product readback. Null/inconsistent responses still fail.
- The product contract and limits are in [history-event-removal.md](history-event-removal.md).
- Favorites membership restoration is implemented locally; see
  [favorites-restore.md](favorites-restore.md). Real isolated-account tests
  seeded movie/show favorites, created complete native backups, removed only
  those test memberships, restored both through the actual development plugin,
  and verified a repeat as a no-op. A second real pass introduced one favorite
  after preview: fresh preflight skipped it and added only the other missing
  membership. Source/restored membership sets matched. All test additions were
  cleaned up; the two original favorite memberships, six ratings and 11 history
  records remained. Original archives/credentials were unchanged. Rank, note
  and timestamp fidelity is explicitly outside this executor's scope.

- Collection restoration now has a read-only dedicated planner; see
  [collection-restore-planner.md](collection-restore-planner.md). Real test-account
  writes/native exports verified missing-movie and changed-date/resolution
  classification through the actual plugin, plus missing-episode detection from
  earlier real archives. Original collection/rating/history/favorite objects were
  unchanged after cleanup. A separate narrow collection apply mode is now implemented. Movie date and four
  technical fields survived; submitted 3d=false returned null. A subsequent actual flat-episode test also preserved its historical date and
  the four technical fields. For both movie/episode, 3d:true read as numeric 1;
  explicit 3d:false read as null. Both test additions were removed and original
  movie/episode/nested-show collections plus ratings/history/favorites were
  exactly unchanged. Other technical enums, hdr and whole-show restoration
  remain unverified. The narrow executor is now implemented locally; see
  [collection-restore.md](collection-restore.md). Actual plugin restoration from
  native source/current ZIPs restored movie/episode dates and four technical
  values; a post-preview conflict refused the entire batch, explicit repeat was
  a verified no-op, restored native observations matched source, and original
  collections/ratings/watches/favorites remained unchanged after test cleanup.

## Discovery acceptance

The discovery CLI and four plugin tools are implemented locally; see
[discovery.md](discovery.md). Ten actual development-plugin reads covered all
four modes for movie/show and popular page two for each type. Trending, popular
and related returned titles; recommendations returned empty arrays on the sparse
test account. A populated real recommendation response remains unverified.
Credential bytes were unchanged; no account writes occurred. Optional complete-history/watchlist exclusion, seen-ID deduplication and
shortlist previews are now implemented. Actual seeded movie/episode tests proved
exclusion and parent-show behavior, and a separate explicitly applied existing
list tool added/read back/removed a selected discovered title. All temporary
history/watchlist entries were removed; original list items/history/watchlist,
ratings/favorites and credential bytes were unchanged. Container updated_at
advanced as expected. Discovery itself never applies list writes.

## Title/season/episode browsing

Implemented locally in CLI and four tools; see [title-browsing.md](title-browsing.md).
Eight real test-account plugin reads passed: movie/show details, season lists,
regular episode list/details and season-0 specials list/details (306 specials).
Aired classification passed live; future/unknown/boundary/offset semantics are
synthetic-tested, not positive live claims. No account writes or credential changes.

## List metadata editing

Implemented locally: `lists update` and `trakt_list_update`; see
[list-metadata-update.md](list-metadata-update.md). Nine actual test-account
plugin cases passed, including explicit-field rename, description set/clear,
private-to-link, no-op, wrong-account/stale refusal and restoration. Original
fields and all four raw list-item objects/order/notes matched baseline afterward;
only updated_at advanced. Credential bytes unchanged. No public/friends live
transition or production-account fallback, and no installation/release.

## List copy/move

Implemented locally in CLI and two tools; see [list-transfers.md](list-transfers.md).
Nine actual plugin cases on two NEW private test lists covered movie/show/season/
episode memberships, repeat/no-op and existing-destination moves. Independent
readback verified source intact for copy and preserved destination records. Both
temporary lists were cleaned; original list metadata/items and credential bytes
unchanged. No production writes or deployment. Notes/timestamps/new ranks are not
copied, and source note-bearing moves refuse before writes. Partial/race guards
are synthetic-tested; no atomic cross-list transaction is claimed.

## Discovery metadata criteria

Implemented locally: genre lookup plus page-local genre/year/runtime/minimum
rating/vote criteria. Nine actual test-account plugin calls passed; ordered
results independently matched original-page audits (7/20 movies, 19/20 shows).
No-match and unknown-genre refusal passed. Live required metadata was complete;
missing/unrated policies remain synthetic-tested. See [discovery.md](discovery.md).
No implicit page refill, global no-match claim, account writes or deployment.

## Bounded watch-tonight shortlists

Implemented locally: `shortlist` and `trakt_watch_tonight`; see
[watch-tonight.md](watch-tonight.md). Eight actual test-account plugin cases
passed, including movie5 across5 pages and show3 across2, budget/no-match stops,
recommendations single-response, exclusion opt-out, related and trending.
Factual predicates, unique IDs and unapplied preview independently checked.
No account writes/credential changes. Partial network/pagination/activity failures
are synthetic-tested; no global completeness, availability or taste claim.

## Next steps

1. Submit/review the empty-statistics fix and the pending restore-planner,
   custom-list-management and ratings/favorites branches in dependency order.
2. Verify repeated-watch preservation with Disable Multiple Plays off, then
   resolve timestamp limitations before promising a faithful restore executor;
   keep the planner read-only.
3. Verify website import on the isolated test account and explicitly separate its
   capabilities from sync API capabilities.
4. Add authorized populated-category and VIP test coverage where available.
5. Keep original archives immutable and distinguish historical supplements from
   current API observations. Category coverage is not byte-for-byte website parity.

## Episode-aware tonight suggestions (November 3)

Implemented locally: `episode-tonight` / `trakt_episode_tonight`. Five real dedicated-test-account plugin cases passed: three verified next episodes, two within 45 minutes, no-fit runtime, one-show budget and specials-option query. Raw progress independently matched returned IDs and incomplete flags; original 11 history events and credential bytes remained unchanged. Positive specials, future/unknown dates and failures remain synthetic-tested. See [full contract](episode-tonight.md). Not installed, submitted or released.

## Preference-seeded related recommendations (November 3)

Local `seeded-related` / `trakt_seeded_related` passed six real dedicated-test-account plugin cases: combined seeds, favorite movies, rating shows, no seeds, seed budget and watchlist opt-out. Independent raw preference/related audits confirmed all returned provenance. Ratings, favorites, history, watchlist and credential bytes remained unchanged. Overlapping multi-seed provenance remains synthetic-tested, not observed live. See [contract](seeded-related.md). Nothing submitted, installed or released.

## Read-only list comparison (November 3)

Two real plugin self-comparisons matched all four existing test-list items; raw metadata/items and credentials unchanged. Distinct-list reconciliation, limits, note-loss protection and changing-revision refusal passed synthetic tests. Only one owned list available; no mutation used to expand fixtures. See [scope](list-comparison.md). Local only.

## Distinct-list comparison acceptance follow-up (November 3)

The earlier distinct-list live-test gap is closed: four actual plugin cases passed using two explicitly authorized disposable private test-account lists. Full/limited/reverse/self results, note conflicts, note-bearing removal withholding and proposal direction/counts matched independent seeded records. Proposals were never applied. Both fixtures were deleted; final list inventory confirmed absence. Original list metadata/four item records and credentials remained exact. See [acceptance evidence](list-comparison.md#distinct-list-live-acceptance). No implementation change, installation or release.
