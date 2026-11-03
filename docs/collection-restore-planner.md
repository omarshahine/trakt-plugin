# Collection restore planner (read-only)

`trakt-cli backup plan-collection --source desired.zip --current current.zip`

OpenClaw: `trakt_backup_plan_collection` with `source` and `current`.
No login or network is needed. There is **no apply flag, sync payload or executor**.

Both ZIPs must pass existing integrity/completeness checks, contain flat
`collection/movies` and `collection/episodes` coverage, match account UUIDs and
provide a current slug. Website-layout completeness is explicitly unknown, not
promoted to a complete native backup. Missing scope cannot imply absence.

Output distinguishes missing entries, existing date/technical-metadata conflicts,
unchanged entries, current-only counts, and blocked invalid source observations.
Missing entries retain observed collected_at and technical metadata. Conflict
reports contain field names, not values. Invalid current details refuse planning;
invalid source dates/metadata are withheld. Dates normalize to UTC; null/omitted
metadata means no observed technical fields. Unknown metadata keys remain visible
for review but are not advertised as writable. Exact archive bytes and deterministic
results are bound to review_id; this is a report fingerprint, not permission to write.

Nested show summaries are not expanded or reconciled: only flat movie/episode
identities are planned. Provider availability, media descriptions, updated_at and
show aggregates are excluded. Current-only entries never become deletion candidates.
Snapshots cannot establish present live state; a future executor needs fresh account,
identity, conflict and outcome checks. Existing entries must not be overwritten.

## Actual isolated-account evidence

Real writes seeded a movie, exported a native backup, changed only its date and
resolution, and exported again. The actual development plugin correctly identified
one missing movie, then exactly collected_at and metadata.resolution conflicts.
Repeated same-archive inspection found no changes. Older actual native backups
also identified a missing episode. Test additions were removed and original collection,
ratings, favorites and history objects remained unchanged.

The movie API preserved the historical date, media_type=digital,
resolution=hd_1080p, audio=dts and audio_channels=5.1 in this test. Submitted 3d=false
returned null; do not promise false-value fidelity. A subsequent real flat-episode probe preserved the same date and four technical
fields. Other enum values, hdr and whole-show writes remain unverified. These observations
are not an exhaustive supported-field contract or permission to enable restoration.


## Follow-up: actual episode and 3d probe

Dedicated test-account writes added one previously absent movie and episode with
historical dates and the four technical values above. Both retained those values.
Submitting `3d:true` returned numeric `1`; an explicit subsequent `3d:false`
returned `null` for both. All other tested fields/dates remained unchanged. The
request payloads and fresh readbacks were retained privately. This establishes
observed wire-format behavior, not that null universally means false or that
unknown/not-set/false are distinguishable. Never silently normalize archive null,
false and zero into the same factual claim.

Both temporary entries were deleted. Exact original movie/episode collection
objects and nested-show collection summaries remained; original ratings,
favorites, history and credential bytes were independently unchanged.

## Proposed first executor contract (not implemented)

- Missing flat movie/episode entries only, exact positive Trakt media IDs and
  known historical collected_at. No whole-show/season expansion or overwrites.
- Bind the exact source/current ZIP bytes, UUID and at-most-100-entry batch to a
  separate executable review digest. The current observation digest alone must
  never authorize application. Explicit apply and expected-account slug required.
- Recheck live slug/UUID and fresh movie/episode collection scopes before writing.
  An existing identical observation may be skipped; a conflicting date or
  technical field refuses the entire reviewed batch before any write.
- Initially permit only absent/null technical data or the demonstrated values:
  media_type=digital, resolution=hd_1080p, audio=dts, audio_channels=5.1. Omit
  absent/null fields without manufacturing values. Other keys/values, non-null
  hdr or 3d, including numeric 1, are withheld until separately supported.
  This intentionally narrow contract is not an exhaustive Trakt enum schema.
- Keep archive 3d observations intact; do not coerce false/0/null or promise
  three-state fidelity. A future explicit mapping needs its own reviewed policy.
- Send only missing rows once, reconcile added/updated acknowledgments and read
  back dates/technical fields plus all pre-existing entries in affected scopes.
  Null technical fields must be checked, not silently ignored as success.
- No server atomic insert-if-absent is assumed. Concurrent conflicting writes,
  uncertain acknowledgments, unexpected updates, partial results and failed
  readbacks must be surfaced. Never automatically replay writes or roll back
  another actor's changes. Current-only entries are never deletion candidates.
- Acceptance: real test-account movie and episode backup/loss/restore, conflict
  after preview, repeat/no-op, unchanged originals, credential/archive hashes and
  independent cleanup. No production-account fallback.
