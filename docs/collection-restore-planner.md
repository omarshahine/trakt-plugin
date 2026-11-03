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
returned null; do not promise false-value fidelity. Episode technical metadata,
other enum values, hdr and whole-show writes remain unverified. These observations
are not an exhaustive supported-field contract or permission to enable restoration.
