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

### 1. Repeated-watch restoration: unresolved

Submitting two different dates for the same movie did not retain two events in
the tested account. Readback returned one event despite the bulk added counts.
A subsequent request returned an updated count, reused the history ID and changed
its date. Both history read routes agreed on one play.

TODO: establish the supported repeated-watch contract and a minimal reproducible
case; investigate account/application behavior without assuming a VIP restriction.
Any future restore executor must verify event multiplicity and dates by readback,
not trust added counts alone. Do not blindly retry uncertain mutations.

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
- Official VIP archive retrieval and a product restore executor are not implemented.
- Removal of an individual history event was exercised by a private API harness;
  a dedicated granular-history-removal CLI/plugin feature remains to be built.

## Next steps

1. Submit/review the empty-statistics fix and the pending restore-planner,
   custom-list-management and ratings/favorites branches in dependency order.
2. Resolve repeated-watch semantics and timestamp limitations before promising a
   faithful restore executor; keep the planner read-only.
3. Verify website import on the isolated test account and explicitly separate its
   capabilities from sync API capabilities.
4. Add authorized populated-category and VIP test coverage where available.
5. Keep original archives immutable and distinguish historical supplements from
   current API observations. Category coverage is not byte-for-byte website parity.
