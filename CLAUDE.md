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

### Automated (CI)

Publishing is automated via GitHub Actions (`.github/workflows/publish-clawhub.yml`). To publish a new version:

```bash
# 1. Bump version in openclaw/package.json
# 2. Commit and push
# 3. Tag and push the tag
git tag -a v1.9.0 -m "Description of changes"
git push origin v1.9.0
```

The workflow extracts the version from the tag name and the changelog from the tag annotation. It publishes the `openclaw/` subdirectory to ClawHub. Authenticates with the `CLAWHUB_TOKEN` repository secret.

**Note**: The existing `push.yml` workflow handles semantic-release, GoReleaser, and npm publish. The `publish-clawhub.yml` workflow runs separately on version tags.

### Manual (fallback)

```bash
./publish-clawhub.sh --changelog "summary of changes"
```

Requires `clawhub` CLI installed (`npm install -g clawhub`) and authenticated (`clawhub login`).

### Verify / Install

```bash
clawhub package inspect trakt-tools
openclaw plugins install trakt-tools
```

## Code Hygiene

- No hardcoded user paths, email addresses, API keys, or PII in tracked files
- Go CLI uses built-in OAuth credentials; users can override via env vars or flags

## Clawpatch Code Review

This repo uses [Clawpatch](https://clawpatch.ai) for local automated code review. Keep `.clawpatch/` ignored; it is generated runtime state containing features, findings, reports, runs, and patch attempts.

Standard workflow:

```bash
clawpatch doctor
clawpatch init          # first time only
clawpatch map
clawpatch review --limit 10
clawpatch report --output .clawpatch/reports/summary.md
clawpatch show --finding <id>
clawpatch fix --finding <id>
clawpatch revalidate --finding <id>
```

If this repo needs hand-authored feature coverage, keep those curated definitions in `tools/clawpatch/features/` and sync/copy them into `.clawpatch/features/` before review. Do not commit `.clawpatch/` generated state.
