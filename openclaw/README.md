# openclaw-trakt

OpenClaw plugin for [Trakt.tv](https://trakt.tv). Track movies and TV shows, view watch history, manage your watchlist, and check show progress.

## Install

```bash
openclaw plugins install trakt-tools
```

## Prerequisites

The `trakt-cli` Go binary must be installed and on your PATH:

```bash
go install github.com/omarshahine/trakt-plugin@latest
```

Or set `TRAKT_CLI_PATH` environment variable, or configure `cliPath` in plugin settings.

## Configuration

| Setting | Description |
|---------|-------------|
| `cliPath` | Path to trakt-cli binary (auto-detected on PATH) |
| `clientId` | Trakt API client ID (from https://trakt.tv/oauth/applications) |
| `clientSecret` | Trakt API client secret |
| `syncEnabled` | Register the opt-in `trakt_sync` tool (default `false`) |

## Available Tools

| Tool | Description |
|------|-------------|
| `trakt_search` | Search for movies and TV shows |
| `trakt_history` | View watch history |
| `trakt_history_add` | Mark movies/shows as watched |
| `trakt_watchlist` | View watchlist |
| `trakt_progress` | Show watch progress for TV shows |
| `trakt_auth` | Start device authorization, then poll its session after approval |
| `trakt_sync` | Opt-in: import from streaming services (only when `syncEnabled`) |

## Authentication

Use `trakt_auth` with `{ "action": "start" }` (or no parameters). Show the
returned `verification_url` and `user_code` immediately. After the user approves,
call it with `{ "action": "poll", "session": "<returned session>" }`, waiting at
least the returned `interval` between pending responses. Stop when the status is
`authenticated`, the session expires, or an error is returned. Keep the session
handle within this authorization flow.

This requires a matching `trakt-cli` with the `auth --session` option. The plugin
checks that capability before starting authentication. On POSIX systems, update
an older CLI or run `trakt-cli auth` locally for interactive setup. On Windows,
complete initial authentication with `trakt-cli auth` locally; nonblocking private
sessions are currently supported on POSIX systems. Configured client credentials
are passed in a separate child environment rather than command-line arguments.

## License

MIT
