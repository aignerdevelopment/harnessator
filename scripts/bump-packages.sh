#!/usr/bin/env bash
# scripts/bump-packages.sh - rewrite the package-manager manifests
# (Formula/harnessator.rb, flake.nix, distribution/aur/PKGBUILD) to a
# released tag, using the release's own checksums file - the single
# truth install.sh already verifies against. The release pipeline runs
# this after every published tag (release.yml, packages job); run it
# manually with the same effect:
#
#   scripts/bump-packages.sh v0.16.0-beta
#
# Markers: manifests carry `# bump:version` / `# bump:sha:<asset>`
# anchors. No marker match -> hard error (a silently skipped bump means
# a tap shipping old binaries forever).
set -eu

TAG=${1:?usage: scripts/bump-packages.sh <tag, e.g. v0.16.0-beta>}
case $TAG in v*) ;; *) echo "tag must start with v: $TAG" >&2; exit 2 ;; esac
REPO=${HARNESSATOR_REPO:-aignerdevelopment/harnessator}
VER=${TAG#v}
cd "$(dirname "$0")/.."

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
SRC=${HARNESSATOR_CHECKSUMS:-https://github.com/$REPO/releases/download/$TAG/harnessator_${TAG}_checksums.txt}
case "$SRC" in
  http*) curl -fsSL "$SRC" -o "$TMP/sums" || { echo "cannot fetch checksums for $TAG - is the release published?" >&2; exit 1; } ;;
  *)     cp "$SRC" "$TMP/sums" ;;
esac

sha_for() { # <darwin_arm64> -> hex
  awk -v key="harnessator_${TAG}_$1.tar.gz" \
      'index($2, key) { print $1 }' "$TMP/sums" | tail -1
}

D_ARM=$(sha_for darwin_arm64); D_AMD=$(sha_for darwin_amd64)
L_ARM=$(sha_for linux_arm64);  L_AMD=$(sha_for linux_amd64)
for pair in "darwin_arm64=$D_ARM" "darwin_amd64=$D_AMD" "linux_arm64=$L_ARM" "linux_amd64=$L_AMD"; do
  val=${pair#*=}
  case $val in
    *[!0-9a-f]*|'') echo "checksums file has no hex sha for ${pair%%=*} of $TAG" >&2; exit 1 ;;
  esac
done

repatch() { # <file> <marker-exists-regex> <sed-expr>
  local f=$1 check=$2 expr=$3 t
  grep -Eq "$check" "$f" || { echo "no '$2' marker in $f - manifest drifted from the bump script" >&2; exit 1; }
  t=$(mktemp)
  sed -E "$expr" "$f" > "$t"
  mv "$t" "$f"
}

f=Formula/harnessator.rb
repatch "$f" 'version "[^"]+" # bump:version' \
  "s|version \"[^\"]+\"( # bump:version)|version \"$VER\"\1|"
for pair in "darwin_arm64=$D_ARM" "darwin_amd64=$D_AMD" "linux_arm64=$L_ARM" "linux_amd64=$L_AMD"; do
  key=${pair%%=*}; val=${pair#*=}
  repatch "$f" "\"[0-9a-f]{64}\" # bump:sha:$key" \
    "s|\"[0-9a-f]{64}\"( # bump:sha:$key)|\"$val\"\1|"
done

# --- flake.nix ---------------------------------------------------------------
f=flake.nix
repatch "$f" 'version = "[^"]+"; # bump:version' \
  "s|version = \"[^\"]+\";( # bump:version)|version = \"$VER\";\1|"
for pair in "darwin_arm64=$D_ARM" "darwin_amd64=$D_AMD" "linux_arm64=$L_ARM" "linux_amd64=$L_AMD"; do
  key=${pair%%=*}; val=${pair#*=}
  repatch "$f" "= \"[0-9a-f]{64}\"; # bump:sha:$key" \
    "s|= \"[0-9a-f]{64}\";( # bump:sha:$key)| = \"$val\";\1|"
done

# --- distribution/aur/PKGBUILD -----------------------------------------------
f=distribution/aur/PKGBUILD
repatch "$f" '^_ver=[^ ]+ # bump:version' \
  "s|^_ver=[^ ]+( # bump:version)|_ver=$VER\1|"
for pair in "linux_arm64=$L_ARM" "linux_amd64=$L_AMD"; do
  key=${pair%%=*}; val=${pair#*=}
  repatch "$f" "\('[0-9a-f]{64}'\) # bump:sha:$key" \
    "s|\('[0-9a-f]{64}'\)( # bump:sha:$key)|('$val')\1|"
done

echo "bumped manifests to $TAG:"
git diff --stat -- Formula flake.nix distribution 2>/dev/null || true
