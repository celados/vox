#!/usr/bin/env bash
set -euo pipefail

# Install vox from GitHub Releases.
# Public repos need only curl; private repos fall back to authenticated gh.
# Override via env: VOX_REPO, VOX_VERSION (tag or "latest"), VOX_INSTALL_DIR.
REPO="${VOX_REPO:-celados/vox}"
VERSION="${VOX_VERSION:-latest}"
BIN_DIR="${VOX_INSTALL_DIR:-$HOME/.local/bin}"

if ! command -v bun >/dev/null 2>&1; then
  echo "vox requires bun: https://bun.sh" >&2
  exit 1
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

if [ "$VERSION" = "latest" ]; then
  ASSET_URL="https://github.com/$REPO/releases/latest/download/vox"
else
  ASSET_URL="https://github.com/$REPO/releases/download/$VERSION/vox"
fi

if ! curl -fsSL --retry 2 -o "$TMP_DIR/vox" "$ASSET_URL"; then
  # Private release assets 404 over plain HTTPS; retry through gh, which
  # carries the user's GitHub auth.
  echo "Direct download failed: $ASSET_URL" >&2
  if ! command -v gh >/dev/null 2>&1; then
    echo "If $REPO is private, install GitHub CLI (https://cli.github.com), run 'gh auth login', and re-run." >&2
    exit 1
  fi
  case "$(gh api "repos/$REPO" --jq .private 2>/dev/null || echo unknown)" in
    true) echo "$REPO is private; falling back to gh release download." >&2 ;;
    *) echo "Retrying via gh release download." >&2 ;;
  esac
  if [ "$VERSION" = "latest" ]; then
    gh release download --repo "$REPO" --pattern vox --dir "$TMP_DIR"
  else
    gh release download "$VERSION" --repo "$REPO" --pattern vox --dir "$TMP_DIR"
  fi
fi

mkdir -p "$BIN_DIR"
install -m 0755 "$TMP_DIR/vox" "$BIN_DIR/vox"

echo "Installed vox to $BIN_DIR/vox"
"$BIN_DIR/vox" --version
