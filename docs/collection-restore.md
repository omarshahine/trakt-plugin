# Missing-only collection restoration

CLI: `backup restore-collections --source desired.zip --current current.zip`
OpenClaw: `trakt_backup_restore_collections`.

Default offline preview produces a separate executable `review_id` bound to exact
validated archive bytes, matching account UUID/current slug, batch limit (1-100)
and withheld observations. The collection observation planner digest is NOT an
apply authorization. To write, explicitly provide `--apply --review-id DIGEST
--expect-user SLUG` (plugin: apply/review_id/expect_user).

Only missing flat movies/episodes and known collected_at dates are eligible.
Technical values are intentionally narrow: digital, hd_1080p, dts and 5.1.
Absent/null known technical fields are omitted, not fabricated. Non-null 3d/HDR,
unknown fields/values, invalid dates and whole-show expansion are unsupported.
Archive conflicts/unsupported source rows are reported as blocked, not overwritten.
Unsupported current scope refuses preview/application instead of inferring absence.

Apply verifies fresh live slug/UUID, bounded complete metadata-extended collection
reads and activity stability. Identical existing targets are skipped; any changed
reviewed target refuses the full batch before writing. Only missing IDs/dates and
allowed technical values are sent, once, to /sync/collection. Readback checks
all targeted values/dates and normalized pre-existing observations in affected
movie/episode scopes. Null/absent technical fields mean unobserved, not a claimed
false value. Top-level server updated_at/provider metadata is not restoration data.

Trakt provides no assumed atomic insert-if-absent: a concurrent actor can change
state between reads/writes. Unexpected updated counts, additions/removals, partial
acknowledgments, failed readback or mismatches never become success. Structured
row states retain known/unknown outcomes. No automatic mutation retry, credential
refresh/replay or rollback. Inspect actual state before any explicit follow-up.

## Actual live acceptance

The real development plugin restored a seeded movie and episode from native ZIPs
with historical dates and four technical fields. A post-preview movie-date
conflict refused the entire batch without adding the episode. After removing the
test conflict, explicit apply verified restoration; repeat was a verified no-op.
A restored native ZIP matched source collection dates/metadata/membership. Test
additions were cleaned; original movie/episode/show collections, ratings, watches,
favorites, credential bytes and original archives remained unchanged.

The first live preflight exposed a plural-path typo and stopped before writing;
corrected singular endpoints passed the acceptance run. No mutation was replayed
to resolve that read-only failure. This is local development, not released or
installed plugin functionality.
