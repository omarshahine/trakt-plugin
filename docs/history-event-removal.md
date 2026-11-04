# Remove one history event

`history remove-event` removes exactly one dated movie or episode play using
its history **event** ID. It does not search titles, accept a show/season, or
fall back to removing all watches of a movie or show.

First read `trakt-cli history --json`. Rows now include `history_id` and
`trakt_id` as strings (avoiding JavaScript integer rounding), with the returned
watch timestamp including any fractional seconds. For episodes, `trakt_id` is
the episode's ID, not the show's ID. Use the returned timestamp: Trakt can
normalize a timestamp submitted when adding a play.

## Preview and apply

Synthetic example — substitute the actual reviewed event, date and account:

```sh
trakt-cli history remove-event --history-id 9001 --type movie --trakt-id 55 --watched-at 2020-01-01T00:00:00Z --expect-user YOUR_ACCOUNT --json
# Only after reviewing and authorizing that exact event:
trakt-cli history remove-event --history-id 9001 --type movie --trakt-id 55 --watched-at 2020-01-01T00:00:00Z --expect-user YOUR_ACCOUNT --apply --json
```

Without `--apply`, preview is offline: no login, network or account changes.
The preview is explicitly **unverified**, not evidence the event exists.
`--apply` requires `--expect-user` to match the freshly authenticated account.
Preflight reads the complete movie/episode-specific history, verifies stable
pagination/activity observations, and matches event ID, media ID/type and date.
An absent event, wrong date/account, unknown date, bad ID or incomplete read
refuses the write. IDs must be canonical positive integers within int64 range.

The only mutation body is `{"ids":[9001]}`. No `movies`, `shows`, `episodes`
or date-based deletion selector is sent. The response counts must describe one
deletion of the expected type. A fresh scoped readback must show the selected
event absent and every other event in that movie/episode scope unchanged.

## Outcomes and limits

- `not_attempted`, `attempted:false`: validation/preflight refused the deletion.
- `unknown`, `attempted:true`, `verified:false`: a POST was attempted but the
  acknowledgement or readback did not prove the expected result. This does not
  mean it is safe to retry. Inspect current history independently first.
- `verified`, `attempted:true`, `verified:true`: scoped readback proved the
  target absent and the other scoped event IDs/types/media/dates/actions unchanged.

No mutation is automatically replayed, including after HTTP 401 or a rate limit.
An already removed event refuses a later apply rather than deleting something
else. Observe Trakt's Retry-After window before further calls after throttling.
The plugin retains structured operation state even when the CLI exits nonzero.

Reads are bounded to 100 pages and 10,000 events in the selected media scope.
Missing/inconsistent pagination, duplicate IDs, invalid records, or activity
changes fail closed. Trakt's empty-history `page_count:0` is accepted only with
page 1, an explicit zero item count, valid limit and an empty array; null is not
an empty history. No sleeps, automatic deletion retries, or broad selector
fallbacks are used. Snapshot/activity checks are not a transactional lock and
do not prove that unrelated media elsewhere in the account never changed.

The legacy `history remove TITLE` command remains a separate **all-plays**
operation. Never use it as fallback for an event-removal failure.

## OpenClaw tool

`trakt_history_remove_event` accepts `history_id`, `type` (`movie` or `episode`),
`trakt_id`, `watched_at`, optional `expect_user` and optional `apply` (false by
default). IDs must be strings. Preview and obtain authorization for the exact
deletion before applying. The new tool is declared in the plugin manifest.
No installed CLI/plugin or Gateway configuration is changed by local development.
