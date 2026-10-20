#!/usr/bin/env bash
# harnessator installer - curl -fsSL https://raw.githubusercontent.com/aignerdevelopment/harnessator/main/install.sh | bash
#
# POSIX-ish bash (also runs under git-bash on Windows, where it installs
# into the MSYS PATH; native Windows users: install.ps1). Installs the
# latest GitHub Release binary, sha256-verified against the release's
# checksums file. Re-run any time to update to the latest; pin with
# --version vX.Y.Z. Never needs root; never runs sudo.
#
# Env overrides: HARNESSATOR_INSTALL_DIR (default ~/.local/bin),
# HARNESSATOR_REPO (for mirrors/forks).
set -eu

REPO="${HARNESSATOR_REPO:-aignerdevelopment/harnessator}"
DEST="${HARNESSATOR_INSTALL_DIR:-$HOME/.local/bin}"
VERSION=""

say() { printf 'harnessator-install: %s\n' "$*"; }
die() { printf 'harnessator-install: error: %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
usage: install.sh [--version vX.Y.Z] [--dir PATH] [--help]

  --version  install a specific release tag (default: latest)
  --dir      install location (default: $HARNESSATOR_INSTALL_DIR or ~/.local/bin)
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --version) [ $# -ge 2 ] || die "--version needs a value"; VERSION="$2"; shift 2 ;;
    --dir)     [ $# -ge 2 ] || die "--dir needs a value";     DEST="$2";     shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1 (see --help)" ;;
  esac
done

command -v curl >/dev/null 2>&1 || die "curl is required"
command -v tar >/dev/null 2>&1  || die "tar is required"

# --- platform detection ---------------------------------------------------
os="$(uname -s)"
case "$os" in
  Linux)  GOOS=linux ;;
  Darwin) GOOS=darwin ;;
  MINGW*|MSYS*|CYGWIN*)
    die "Windows detected under git-bash. Native install: run install.ps1 in PowerShell, or grab the windows .zip from https://github.com/$REPO/releases/latest" ;;
  *) die "unsupported OS: $os" ;;
esac
arch="$(uname -m)"
case "$arch" in
  x86_64|amd64)  GOARCH=amd64 ;;
  arm64|aarch64) GOARCH=arm64 ;;
  *) die "unsupported architecture: $arch" ;;
esac

# --- version resolution (tags are embedded in asset names) ----------------
if [ -z "$VERSION" ]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)"
  [ -n "$VERSION" ] || die "could not resolve the latest release - pass --version explicitly"
fi

ext="tar.gz"
asset="harnessator_${VERSION}_${GOOS}_${GOARCH}.${ext}"
base="https://github.com/$REPO/releases/download"

# --- checksum verification -------------------------------------------------
sha_cmd=""
command -v sha256sum >/dev/null 2>&1 && sha_cmd="sha256sum"
[ -z "$sha_cmd" ] && command -v shasum >/dev/null 2>&1 && sha_cmd="shasum -a 256"
[ -n "$sha_cmd" ] || die "need sha256sum or shasum to verify the download - refusing to install unverified"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

say "fetching $asset"
curl -fsSL -o "$tmp/$asset" "$base/$VERSION/$asset" || die "download failed: $base/$VERSION/$asset"
curl -fsSL -o "$tmp/checksums.txt" "$base/$VERSION/harnessator_${VERSION}_checksums.txt" \
  || die "checksums file not found for $VERSION - refusing to install unverified"

line="$(grep -E "(^| )${asset}\$|/${asset}\$" "$tmp/checksums.txt" | tail -1)" \
  || die "no checksum line for $asset - refusing"
want="$(printf '%s' "$line" | cut -d' ' -f1)"
got="$($sha_cmd "$tmp/$asset" | cut -d' ' -f1)"
[ "$want" = "$got" ] || die "checksum MISMATCH for $asset (want $want, got $got) - refusing to install"
say "checksum verified"

# --- install -----------------------------------------------------------------
mkdir -p "$DEST"
tar xzf "$tmp/$asset" -C "$tmp"
mv "$tmp/harnessator_${VERSION}_${GOOS}_${GOARCH}" "$DEST/harnessator"
chmod 755 "$DEST/harnessator"

say "installed $("$DEST/harnessator" version 2>/dev/null || echo "$VERSION") -> $DEST/harnessator"

# --- PATH guidance (never edit the user's shell rc unasked) ----------------
case ":$PATH:" in
  *":$DEST:"*) : ;;
  *) printf '\n  %s is not on your PATH. Add it, e.g.:\n\n    export PATH="%s:$PATH"   # and save to ~/.zshrc / ~/.bashrc\n\n' "$DEST" "$DEST" ;;
esac

say "done. Next: 'harnessator init --help' or the guides at https://github.com/$REPO/tree/main/docs/guides"
