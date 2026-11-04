# Verified copy/move between owned lists

```sh
trakt-cli lists copy --source 11 --destination 22 --type movie --ids 55,56 --json
trakt-cli lists move --source 11 --destination 22 --type episode --ids 88 --apply --expect-user YOUR_ACCOUNT_SLUG --json
```

Tools: `trakt_list_copy`, `trakt_list_move`. Default offline preview; explicit
apply and expected account required to write. Source/destination are different
canonical numeric LIST IDs; ids are MEDIA IDs, not entry IDs. One type per batch:
movie/show/season/episode, 1-100 unique IDs. No whole-list deletion tool is added.

## Membership-only scope

- Copy leaves the source intact; move removes selected source memberships only
  after destination verification. Existing destination entries are never re-added.
- Notes are NOT copied. Move refuses any selected source entry with nonempty or
  unsupported notes, before any write. VIP note transfer is not implemented.
- Listed dates, new entry IDs and new absolute ranks are server-assigned. No
  historical fidelity is claimed. Unrelated existing entry data and relative
  ordering, plus list settings, are verified. Rank values can renumber on removal.
- Source/destination list names, descriptions, privacy, sort/display settings are
  not changed. No title guessing or automatic public-list creation.

## Verification sequence

1. Validate input offline; resolve fresh token identity and both list owners/IDs.
2. Read each list's metadata/items/metadata with strict bounded pagination.
   Missing/changed state, duplicate entries/media/ranks and unknown identities
   refuse. Same bounds as list metadata editing: 100 pages/100000 entries.
3. Require every selected source membership. For move only, source-absent and
   destination-present is an already-at-destination no-op state, not proof of
   historical provenance. Both absent refuses the entire batch before writing.
4. Add only missing destination memberships once. Any uncertain/bad add result
   stops without attempting source removal, even if some entries may exist.
5. Read back destination expected membership and preserved data/settings; re-read
   source and refuse if it changed. Copy can finish here.
6. For move, recheck destination immediately before source removal. A changed or
   unverified destination stops without removal. Remove only selected currently
   present source media, once, then verify both final sets and preserved data.

There is no atomic cross-list transaction or server lock. A concurrent actor can
change state after a read. Errors retain stage, selected IDs, add_attempted,
remove_attempted, destination_verified and partial/unknown outcomes. The destination
flag describes a completed observation, not a perpetual guarantee. No automatic
mutation retry, token-refresh replay or rollback. Inspect BOTH lists before an
explicit follow-up. Repeating a completed operation verifies a no-op rather than
blindly replaying writes.

## Actual live acceptance

Two NEW private test-account lists were used, not the original list. Nine real
plugin cases passed: preview, wrong-account refusal, movie copy/repeat copy,
movie move into an existing destination/repeat move, episode add-then-remove
move, show copy and season add-then-remove move. Independent raw readback proved
source intact after copy, existing/unrelated destination entries exact and final
memberships correct. Both temporary lists were deliberately deleted by a guarded
cleanup harness. Original list metadata/all four items and credentials remained
exact. No production fallback, installation, publication or deployment occurred.

Race/partial paths are synthetic-tested: failed add/readback, source/destination
changes and unverified destination prevent source deletion. Uncertain removal or
unexpected surviving entry changes never produce verified success.
