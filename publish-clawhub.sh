#!/bin/bash
set -euo pipefail

# Publish trakt-tools to ClawHub
# Usage: ./publish-clawhub.sh [--changelog "description of changes"]

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PKG_DIR="$SCRIPT_DIR/openclaw"
PKG_JSON="$PKG_DIR/package.json"

VERSION=$(node -p "require('$PKG_JSON').version")
COMMIT=$(git rev-parse HEAD)

# Parse --changelog argument or prompt
CHANGELOG=""
while [[ $# -gt 0 ]]; do
  case $1 in
    --changelog)
      CHANGELOG="$2"
      shift 2
      ;;
    *)
      echo "Unknown argument: $1"
      echo "Usage: $0 [--changelog \"description of changes\"]"
      exit 1
      ;;
  esac
done

if [[ -z "$CHANGELOG" ]]; then
  echo "Version: $VERSION"
  echo "Commit:  $COMMIT"
  echo ""
  read -rp "Changelog: " CHANGELOG
fi

if [[ -z "$CHANGELOG" ]]; then
  echo "Error: changelog is required"
  exit 1
fi

echo "Publishing trakt-tools v$VERSION to ClawHub..."
echo "  Commit: $COMMIT"
echo "  Changelog: $CHANGELOG"
echo ""

(cd "$PKG_DIR" && npm install && npm run build)

clawhub package publish "$PKG_DIR" \
  --family code-plugin \
  --name "trakt-tools" \
  --display-name "Trakt" \
  --version "$VERSION" \
  --changelog "$CHANGELOG" \
  --tags "latest" \
  --source-repo "omarshahine/trakt-plugin" \
  --source-commit "$COMMIT" \
  --source-ref "main" \
  --source-path "openclaw"

echo ""
echo "Published trakt-tools v$VERSION to ClawHub."
echo "Verify: clawhub package inspect trakt-tools"
