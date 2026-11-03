# Ratings-only restore executor

`backup restore-ratings` restores **missing, dated ratings only**. It does not
restore history, watchlists, collection, favorites, custom lists or account
settings. It is not a general backup importer or a website-import executor.

## Review offline, then explicitly apply

```sh
trakt-cli backup restore-ratings --source desired.zip --current current.zip --limit 100 --json
# Review the account, selected ratings, blocked rows and remaining count.
# Copy that exact review_id; do not guess or reuse a different batch's digest.
trakt-cli backup restore-ratings --source desired.zip --current current.zip --limit 100 --review-id REVIEW_ID --expect-user YOUR_ACCOUNT --apply --json
```

Without `--apply`, the command loads no credentials, contacts no service, and
changes neither archives nor account data. Both ZIPs use the archive reader's
CRC/JSON/identity checks; native ZIPs additionally require complete manifests,
hashes and counts. Website-layout archives still have unknown completeness.
Both archives must identify the same nonempty account UUID; a current account
slug is also required. Slug-only matching is insufficient for this executor.

The preview selects only conflict-free missing movie/show/season/episode
ratings from the existing read-only planner. Ratings must have a valid 1-10
integer value and an explicit valid timestamp. No timestamp is invented.
Invalid/conflicting/unsupported ratings are reported under `blocked`; other
categories are not selected. Already existing ratings in the supplied current
archive are not restore candidates. **A zero selected count is not evidence
that every backup category or every blocked rating has been restored.**

`limit` is 1-100. Selection is deterministic in the planner's resource/ID order;
`remaining` counts eligible ratings beyond this batch. The SHA-256 `review_id`
binds the exact source/current ZIP bytes, UUID/slug, selected batch, limit and
blocked/remaining information. The parser uses bounded immutable in-memory file
snapshots, so the digest describes the bytes that were actually validated.
Changing either file or the batch options requires a new preview. Path changes
alone do not alter the review ID when file bytes are identical.

## Applied behavior

Before writing, `expect-user` must match the reviewed slug, and live settings
must match both that slug and the archive UUID. Complete, fresh paginated reads
of the affected rating types are bounded by account-activity observations.

- A target already matching the exact rating and normalized timestamp is skipped.
- Any target with a different current value **or date** refuses the entire batch
  before a POST; other missing candidates are not partially started.
- Remaining missing rows are sent in one `/sync/ratings` POST, with their own
  exact media IDs, rating values and timestamps. No unrelated selectors are sent.
- Acknowledgement must report the expected additions, no updates, and no missing
  items. Fresh readback must match every selected value/date and leave all prior
  rating values/dates in affected scopes unchanged, without unexpected additions.
- An explicit rerun rechecks live state and skips completed exact matches. A
  fully completed batch returns `noop`, `attempted:false`; it is not reposted.

After completing a capped batch, create a **new current backup and preview** to
select the next batch. Reusing the old current archive selects the same batch
and intentionally becomes a no-op when those ratings already match.

## Races, partial results and bounds

Trakt has **no atomic insert-if-absent/conditional rating write**. There is a
preflight-to-write race window, not a transactional lock. Another client could
rate a target in that interval; the service may then update it. Unexpected
updated counts, changed prior ratings, date normalization, missing targets or
incomplete readback never produce verified success. This limitation cannot be
eliminated by client-side checks; avoid concurrent account edits during restore.

Results preserve each reviewed row's state (`missing`, `conflict`, `unknown`,
`already_present`, `present_verified`, or `different`) plus `attempted`,
`verified` and `outcome`. A POST acknowledgement is not proof of restored data.
`partial`/`unknown` can mean changes occurred: inspect row states/current account
before deciding on a new explicit apply. No automatic retry, rollback, token-
refresh replay of the POST, or conflict overwrite is attempted. Rate limits stop
the operation; observe Retry-After before calling Trakt again. Uncertain CLI
results remain structured in the plugin even when the process exits nonzero.

Reads require explicit consistent pagination and arrays (null is not empty).
They are capped at 100 pages/100,000 records per affected media type, with bounded
response bodies. Activity changes, malformed rows and duplicate IDs fail closed.
These checks concern affected rating scopes, not a guarantee about every other
account resource. Source archives remain immutable. There is no automatic resume
or scheduled execution, and no saved credential is embedded in a review/result.

## OpenClaw

`trakt_backup_restore_ratings` exposes `source`, `current`, optional `limit`,
optional `apply` (false), `review_id` and `expect_user`. Preview and obtain
authorization for the exact batch before applying. Review/output files contain
private account IDs, ratings and dates; keep them private. The feature is local
development until the corresponding CLI/plugin changes are released/installed.
