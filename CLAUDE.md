# trakt-plugin

CLI and AI agent plugin for Trakt.tv. Go CLI at repo root, OpenClaw plugin in `openclaw/`, Claude Code plugin via `.claude-plugin/` and `skills/`.

## Quick Commands

```bash
# Build Go CLI
go build -o trakt-cli .

# Run OpenClaw plugin locally
openclaw plugins install -l ./openclaw
```

## Repo Layout

| Path | Purpose |
|------|---------|
| `main.go` | Go CLI entry point |
| `cmd/` | CLI command implementations |
| `api/` | Trakt API client (Go) |
| `openclaw/` | OpenClaw plugin package (ClawHub: trakt-tools) |
| `openclaw/src/index.ts` | OpenClaw tool registration |
| `.claude-plugin/` | Claude Code plugin manifest |
| `skills/` | Claude Code skill definitions |
| `marketplace.json` | ClawHub marketplace metadata |

## Publishing (OpenClaw Plugin)

The OpenClaw plugin is published as `trakt-tools` on ClawHub.

### Prerequisites

- `clawhub` CLI installed: `npm install -g clawhub`
- Authenticated: `clawhub login` (browser OAuth flow)
- Verify: `clawhub whoami`
- `openclaw/package.json` must have `openclaw.compat.pluginApi` and `openclaw.build.openclawVersion` fields

### Publish Script (preferred)

```bash
./publish-clawhub.sh --changelog "summary of changes"
```

The script extracts the version from `openclaw/package.json`, gets the current git SHA, and calls `clawhub package publish` with all required flags. If `--changelog` is omitted, it prompts interactively.

### Manual Publish Command

```bash
clawhub package publish ./openclaw \
  --family code-plugin \
  --name "trakt-tools" \
  --display-name "Trakt" \
  --version <VERSION> \
  --changelog "<description of changes>" \
  --tags "latest" \
  --source-repo "omarshahine/trakt-plugin" \
  --source-commit $(git rev-parse HEAD) \
  --source-ref "main" \
  --source-path "openclaw"
```

### Verify Publication

```bash
clawhub package inspect trakt-tools
```

### Install (end user)

```bash
openclaw plugins install trakt-tools
```

## Code Hygiene

- No hardcoded user paths, email addresses, API keys, or PII in tracked files
- Go CLI uses built-in OAuth credentials; users can override via env vars or flags
