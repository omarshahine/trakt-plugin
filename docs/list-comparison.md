# Read-only list comparison and reconciliation previews

`lists compare --source 10 --destination 20 --limit 20 --json` and `trakt_list_compare` read two owned numeric list IDs. Self-comparison is allowed. No apply flag, list writes, token refresh/persistence or automatic synchronization.

Membership is keyed by media type plus exact ID, not title or rank. Results include source-only, destination-only and shared entries, note differences, revisions and membership hashes. Shared rows carry source entry details; note differences are warnings, not note merges. No claim of metadata/rank/date equality follows from shared membership.

Source-only entries produce membership-only copy previews to destination. Destination-only entries produce removal previews from destination, never from source. These are separate optional alternatives, not automatic deletion authorization. Each direction has its own 1-100 item limit, selected in original list order and grouped by type. Omitted counts and withheld reasons expose incomplete proposal batches. Person proposals and note-bearing removals are withheld. Copy does not preserve notes, dates or new ranks.

Complete strict list snapshots use existing ownership and consistency checks, then both lists are reread before returning proposals. Any observed revision/item/settings change refuses the report. Maximum 1000 entries per list for bounded report size. This is not an atomic cross-list snapshot. Hashes/revisions are diagnostic observations, not executable review tokens. Applying anything requires separate explicit authorization and fresh verification through existing mutation tools; a stale preview is never permission to remove data.

Verification: synthetic distinct-list cases cover typed overlap, notes conflicts, note-loss refusal, unsupported people, rank-ordered proposal limits, empty/self comparison and revision changes. Two actual dedicated-test-account plugin calls (limits 1 and 20) compared its one existing list to itself and independently matched all four shared entries. Original raw metadata, item records and credential bytes remained exact. That initial read-only checkpoint had no second owned list; the subsequent authorized disposable-list acceptance below closes that gap. No account mutations or production fallback. Local development only; not submitted or deployed.


## Distinct-list live acceptance

After explicit authorization, two disposable private lists were created on the dedicated test account. Source had three items; destination had four. Fixtures included one shared movie with differing notes, two source-only memberships and three destination-only memberships, one with a note that must not be discarded.

Four actual development-plugin cases passed:

- Full comparison: one shared, two source-only, three destination-only; two copy and two removal preview items; the note-bearing removal was withheld and the shared note conflict reported.
- Limit one: one proposal item per direction, with one eligible item omitted per direction and the note-bearing removal still withheld.
- Reverse direction: three copy and two removal preview items, with correct source/destination roles.
- Self-comparison: all three entries shared; zero mutation proposals.

All proposals remained unapplied. Both fixture lists' raw item records were unchanged by comparison. Both temporary lists were then deleted after exact ID/name/owner/privacy guards, and the final owned-list read confirmed their absence. Original list raw metadata and all four item records, plus credential bytes, matched the pre-test snapshots exactly. No production-account access or deployment. Private receipts are not committed. No implementation changes were needed.
