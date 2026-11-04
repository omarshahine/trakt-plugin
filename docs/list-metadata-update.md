# Preview-first list metadata editing

```sh
trakt-cli lists update --list 55 --name "Evening shortlist" --json
trakt-cli lists update --list 55 --description "" --json
trakt-cli lists update --list 55 --privacy private --apply --expect-user YOUR_ACCOUNT_SLUG --json
```

OpenClaw: `trakt_list_update`. Numeric LIST ID required, not a display name,
list-entry/media ID or slug: the stable ID survives a rename. Resolve the list
with `trakt_lists` first. At least one explicit name/description/privacy field is
required. Defaults are offline previews, no login/network.

## Explicit field semantics

- Omitted fields are not sent and must remain unchanged on readback.
- Name cannot be blank; client bound 255 Unicode characters and valid UTF-8.
- Explicit empty description clears it; omission preserves it. Client bound
  10000 Unicode characters. Server limits can still refuse a request.
- Privacy accepts private/link/friends/public. It NEVER defaults to private
  during update. Visibility changes require authorization for that exact setting.
- No implicit changes to item membership, notes, ranking, sort/display settings
  or comment preferences. List deletion is not part of this command.

## Apply and verification

Apply requires explicit `apply`/`--apply` and `expect_user`/`--expect-user`.
Fresh token identity and returned list owner/ID must match. A caller may provide
`expect_updated_at`/`--expect-updated-at` from a recent list read to reject a stale
revision. Preview uses unresolved owner me; it is not a lock or an account proof.

Before writing, read list metadata, complete bounded item pages, then metadata
again. Refuse incomplete/changed state, unsupported identities and duplicate
entries. Item signatures retain entry/type/media IDs, rank, listed_at and notes,
not dynamic catalog-child fields. Bounds are 100 pages/100000 entries, 4 MiB per
page and 2 MiB metadata. Missing/malformed consistency fields refuse application.

Only supplied metadata fields are PUT once. Matching current values produce a
verified no-op with no PUT. After HTTP200, repeat stable reads and verify all
requested values, protected unrequested settings, count and item signature.
Wrong accounts, wrong ownership and stale revisions refuse before writing.
Response/readback failures leave structured unknown/partial outcomes. Never
blindly retry or automatically roll back.

No server transactional lock or compare-and-swap is assumed. Changes can race
after preflight; unexpected readback is reported, not hidden. The optional revision
guard cannot prevent a later concurrent change. Server updated_at and possibly
server-derived slug/share metadata are not promised byte-identical. No automatic
credential refresh/persistence or mutation replay occurs.

## Actual test-account acceptance

Nine real development-plugin cases passed: offline preview; wrong-account and
stale-revision refusal; name-only rename; repeat no-op; description-only set and
explicit clear; privacy-only private-to-link; restoration of original fields.

Final original name/description/private visibility, sort/display/comment settings,
created_at, type and all four raw item rows/order/notes matched baseline exactly.
Only updated_at advanced. Credentials unchanged, production account untouched.
Public/friends transitions were not applied live; enum/input handling is synthetic-
tested. Installation, publication and deployment remain separate and were not done.
