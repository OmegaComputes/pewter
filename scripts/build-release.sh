#!/bin/sh
# Build release archives, SHA256SUMS and (optionally) a signature. LOCAL ONLY:
# this script does not upload, publish or tag anything.
#
#   scripts/build-release.sh <version> [outdir]
#
# Env:
#   PEWTER_TARGETS      space separated os/arch list (default: linux/amd64 linux/arm64 darwin/amd64 darwin/arm64)
#   PEWTER_SIGNING_KEY  path to an Ed25519 private key (PEM). Signs SHA256SUMS and latest.txt.
#                       The real release key must never live on a build box; use a test key here.
set -eu
VERSION="${1:?usage: build-release.sh <version> [outdir]}"
VERSION="${VERSION#v}"
OUT="${2:-dist}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGETS="${PEWTER_TARGETS:-linux/amd64 linux/arm64 darwin/amd64 darwin/arm64}"
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo none)"
DATE="${PEWTER_BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

mkdir -p "$OUT"
OUT="$(cd "$OUT" && pwd)"
rm -f "$OUT"/pewter-v"$VERSION"-*.tar.gz "$OUT/SHA256SUMS" "$OUT/SHA256SUMS.sig" "$OUT/latest.txt" "$OUT/latest.txt.sig"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

for t in $TARGETS; do
  os="${t%/*}"; arch="${t#*/}"
  d="$STAGE/$os-$arch"; mkdir -p "$d"
  ( cd "$ROOT" && CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
      -ldflags "-s -w -X github.com/OmegaComputes/pewter/internal/version.Version=$VERSION -X github.com/OmegaComputes/pewter/internal/version.Commit=$COMMIT -X github.com/OmegaComputes/pewter/internal/version.Date=$DATE" \
      -o "$d/pewter" ./cmd/pewter )
  # Apache-2.0 asks that redistributed binaries carry the license and NOTICE.
  [ -f "$ROOT/LICENSE" ] && [ -f "$ROOT/NOTICE" ] || { echo "LICENSE and NOTICE must exist in $ROOT" >&2; exit 1; }
  cp "$ROOT/LICENSE" "$ROOT/NOTICE" "$d/"
  chmod 0644 "$d/LICENSE" "$d/NOTICE"
  name="pewter-v$VERSION-$os-$arch.tar.gz"
  # Archive = LICENSE, NOTICE, pewter (exactly these three, in this order).
  # Fixed ordering, owner and mtime so the archive bytes depend only on the inputs.
  LC_ALL=C tar -C "$d" --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner -cf - LICENSE NOTICE pewter | gzip -n > "$OUT/$name"
  echo "built $name"
done

( cd "$OUT" && sha256sum pewter-v"$VERSION"-*.tar.gz > SHA256SUMS )
printf '%s\n' "$VERSION" > "$OUT/latest.txt"

if [ -n "${PEWTER_SIGNING_KEY:-}" ]; then
  for f in SHA256SUMS latest.txt; do
    openssl pkeyutl -sign -rawin -inkey "$PEWTER_SIGNING_KEY" -in "$OUT/$f" -out "$OUT/$f.sig"
  done
  echo "signed SHA256SUMS and latest.txt"
else
  echo "NOT signed (no PEWTER_SIGNING_KEY): install.sh will refuse these files" >&2
fi
echo "output in $OUT"
