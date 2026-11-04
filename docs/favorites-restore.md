# Favorites membership restoration

`backup restore-favorites` restores **missing movie/show favorites membership**,
not a complete favorites snapshot. It never restores, clears or sends ranks,
notes or timestamps. Trakt assigns metadata/order to new additions and may
renumber ranks; do not treat membership equality as metadata fidelity.

```sh
trakt-cli backup restore-favorites --source desired.zip --current current.zip --limit 100 --json
# Review the exact batch and copy its review_id before an authorized apply:
trakt-cli backup restore-favorites --source desired.zip --current current.zip --limit 100 --review-id REVIEW_ID --expect-user YOUR_ACCOUNT --apply --json
```

Default preview is offline: no login, network or file/account writes. Exact
source/current archive bytes, UUID/current slug, selected batch, limit,
blocked/remaining information, membership-only kind and out-of-scope fields
are bound to a SHA-256 review ID. Source/current files remain immutable.
Changing bytes or batch options requires a new preview. Only the current
account's explicit expected slug and matching live UUID can authorize a write.

Both ZIPs must contain favorites coverage and the same nonempty account UUID.
ZIP/CRC/JSON/identity checks apply; native archives also require complete
manifest hashes/counts. Website-layout ZIP completeness remains unknown.
Unsupported source types/IDs are withheld under `blocked`; unsupported or
ambiguous current membership refuses preview rather than inferring absence.
Duplicate typed memberships are refused. Movies and shows are separate ID
namespaces; IDs remain canonical positive int64 strings at tool boundaries.

Selection is deterministic and capped at 1-100 memberships per batch.
Existing membership in the supplied current ZIP is not selected, regardless
of rank/note/timestamp differences. `remaining` counts eligible rows beyond
the selected batch. After a successful capped batch, take a fresh current
backup and preview again to select the next batch. A no-op on an old batch
does not mean all other backup categories or blocked memberships were restored.

## Applied behavior and verification

1. Verify the freshly authenticated slug and UUID against the reviewed account.
2. Read complete fresh movie/show favorites membership, bounded by activity
   observations. Skip any reviewed membership already present, including one
   added after preview. No existing favorite is removed or deliberately reposted.
3. Refuse the entire batch before writing if the known live account capacity
   would be exceeded. Missing/zero capacity metadata is not assumed unlimited;
   the service can still reject an attempted addition. A 420/429 is not retried.
4. POST only missing movie/show media IDs to `/sync/favorites`. No removal,
   rank, note or timestamp fields are sent. Server `existing` counts can be
   legitimate if another client added a target during the preflight/write gap.
5. Validate acknowledgement counts/not-found results, then read back complete
   membership. Every selected target must be present, all prior memberships
   must remain, and the observed set must equal the expected union.

`verified` means the membership readback passed, **not** that metadata was
restored. A completed explicit repeat is a verified `noop`, `attempted:false`.
Partial/uncertain results retain per-row states (`already_present`, `missing`,
`unknown`, `present_verified`) and attempted/verified flags. The plugin keeps
those states even on a nonzero CLI exit or rate limit.

No write is automatically retried, rolled back, or expanded to other resources.
Inspect partial/uncertain results before deciding on a new explicit apply;
completed memberships will then be freshly checked/skipped. Observe Retry-After
before further Trakt calls when throttled. An acknowledgement alone never
establishes restored membership. Activity checks are not a transactional lock;
concurrent changes or incomplete readback can produce uncertainty without an
automatic corrective mutation.

Reads require explicit consistent pagination and JSON arrays (null is not empty),
are capped at 100 pages/100,000 records, and reject invalid/duplicate membership.
The service's consistent empty-first-page `page_count:0` representation is
supported without weakening nonempty pagination checks.

## OpenClaw

`trakt_backup_restore_favorites` accepts `source`, `current`, optional `limit`,
`review_id`, `expect_user` and `apply` (default false). Preview and obtain
authorization for the exact batch before applying. It does not restore ratings,
history, watchlists, collection, custom lists, account settings or VIP fields.
Reviews/receipts contain private account IDs; keep them private. Local development
does not replace an installed plugin/CLI or restart the Gateway.
