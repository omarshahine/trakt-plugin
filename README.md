# trakt-cli

```

████████╗██████╗  █████╗ ██╗  ██╗████████╗     ██████╗██╗     ██╗
╚══██╔══╝██╔══██╗██╔══██╗██║ ██╔╝╚══██╔══╝    ██╔════╝██║     ██║
   ██║   ██████╔╝███████║█████╔╝    ██║       ██║     ██║     ██║
   ██║   ██╔══██╗██╔══██║██╔═██╗    ██║       ██║     ██║     ██║
   ██║   ██║  ██║██║  ██║██║  ██╗   ██║       ╚██████╗███████╗██║
   ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝   ╚═╝        ╚═════╝╚══════╝╚═╝
```

A CLI for [trakt.tv](https://trakt.tv) using the [trakt.tv API](https://trakt.docs.apiary.io/).

Forked from [angristan/trakt-cli](https://github.com/angristan/trakt-cli) with added commands (`watchlist`, `progress`, `history add`), `--json` output on all commands, and plugin packaging for [OpenClaw](https://docs.openclaw.ai/) and [Claude Code](https://claude.ai/claude-code).

![](https://user-images.githubusercontent.com/11699655/154494260-d3ff23ec-72b2-45e4-9f39-41f52119621b.png)

## Installation

### From source

```bash
go install github.com/omarshahine/trakt-cli@latest
```

### From releases

Grab a binary from the [releases](https://github.com/omarshahine/trakt-cli/releases).

### Development

```bash
git clone https://github.com/omarshahine/trakt-cli
cd trakt-cli
go build
```

## Authentication

Create a Trakt API app at https://trakt.tv/oauth/applications/new to get a Client ID and Client Secret, then:

```
trakt-cli auth --client-id xxx --client-secret yyy
Please go to https://trakt.tv/activate and enter the following code: XXXXXXXX
Successfully authenticated, creds written to ~/.trakt.yaml
```

## Commands

All commands support `--json` for machine-readable output.

### search

Search for movies and TV shows.

```bash
trakt-cli search "Severance" --json
trakt-cli search "Inception" --type movie --json
```

`--type`: `movie`, `show`, or `movie,show` (default)

### history

View watch history.

```bash
trakt-cli history --json
trakt-cli history --type shows --limit 20 --json
trakt-cli history --type movies --json
```

### history add

Mark movies or shows as watched.

```bash
trakt-cli history add "Severance" --json
trakt-cli history add "The Sopranos" "The Wire" --json
trakt-cli history add --type movie "The Godfather" --json
trakt-cli history add --watched-at 2025-06-15 "Dark" --json
```

- Searches by title, prefers exact matches
- `--type`: `show` (default) or `movie`
- `--watched-at`: YYYY-MM-DD or RFC3339 (defaults to now)
- Accepts multiple titles in one call

### watchlist

View your watchlist.

```bash
trakt-cli watchlist --json
trakt-cli watchlist --type shows --limit 100 --json
trakt-cli watchlist --type movies --json
```

### progress

Show progress of watchlist TV shows — which are in-progress, not started, or completed.

```bash
trakt-cli progress --json
trakt-cli progress --all --json
```

- Default: in-progress items only + summary counts
- `--all`: includes not_started and completed lists

## AI Agent Plugins

This repo ships plugin packaging for two AI agent platforms:

### OpenClaw Plugin

Native tool-based plugin that registers typed tools (`trakt_search`, `trakt_history`, `trakt_watchlist`, `trakt_progress`, `trakt_history_add`).

```bash
openclaw plugins install -l ./openclaw
openclaw gateway restart
```

See `openclaw/` for the plugin source.

### Claude Code Plugin

Skill-based plugin that teaches Claude Code how to use the CLI via exec.

```bash
claude --plugin-dir /path/to/trakt-cli
```

See `.claude-plugin/` and `skills/` for the plugin source.

## License

MIT
