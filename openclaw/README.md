# openclaw-trakt

OpenClaw plugin for [Trakt.tv](https://trakt.tv). Track movies and TV shows, view watch history, manage your watchlist, and check show progress.

## Install

```bash
openclaw plugins install openclaw-trakt
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

## Available Tools

| Tool | Description |
|------|-------------|
| `trakt_search` | Search for movies and TV shows |
| `trakt_history` | View watch history |
| `trakt_history_add` | Mark movies/shows as watched |
| `trakt_watchlist` | View watchlist |
| `trakt_progress` | Show watch progress for TV shows |
| `trakt_auth` | Set up Trakt.tv authentication |

## License

MIT
