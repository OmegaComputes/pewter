#!/bin/sh
# Pewter CLI installer. Served at https://omegacompute.com/install.
#
#   curl -fsSL https://omegacompute.com/install | sh
#
# What it does: downloads a release archive, verifies an Ed25519 signature on
# the checksum list and then the SHA-256 of the archive, and only then
# installs `pewter` plus its short alias `pew`. If anything does not verify,
# it installs nothing. It does not edit your shell profile and does not need root.
#
# Prefer to read it first?  curl -fsSLo install.sh https://omegacompute.com/install && sh install.sh
# When piped, this script's own bytes are protected by TLS only; the program it
# installs is verified either way.
#
# Options:  --version X.Y.Z   install a specific version (default: latest.txt, signed)
#           --dir DIR         install directory (default ~/.local/bin, or /usr/local/bin as root)
#           --dry-run         download and verify, install nothing
#           --verbose         show progress (also PEWTER_VERBOSE=1)
#           --help
# Env:      PEWTER_BASE_URL (default https://omegacompute.com/cli; https, or http on loopback)
#           PEWTER_RELEASE_PUBKEY_FILE  use a different public key (testing only)
set -eu

BASE_URL="${PEWTER_BASE_URL:-https://omegacompute.com/cli}"
BINARY="pewter"
VERSION="${PEWTER_VERSION:-}"
INSTALL_DIR="${PEWTER_INSTALL_DIR:-}"
DRY_RUN=0
VERBOSE="${PEWTER_VERBOSE:-0}"
ALIAS="pew"

# Release public key (Ed25519, PEM). PLACEHOLDER until the first signed release:
# Heir Pte Ltd generates the signing key offline and publishes its public half
# here. While it is a placeholder this script refuses to install anything.
RELEASE_PUBKEY='-----BEGIN PUBLIC KEY-----
PLACEHOLDER-RELEASE-KEY-NOT-CONFIGURED
-----END PUBLIC KEY-----'

err() { printf 'pewter-install: error: %s\n' "$1" >&2; exit 1; }
info() { [ "$VERBOSE" = 1 ] && printf '%s\n' "$1" >&2; return 0; }
warn() { printf '%s\n' "$1" >&2; }
say() { printf '%s\n' "$1" >&2; }
need() { command -v "$1" >/dev/null 2>&1 || err "need '$1' (not found)"; }

while [ $# -gt 0 ]; do
  case "$1" in
    --version) [ $# -ge 2 ] || err "--version needs a value"; VERSION="$2"; shift 2 ;;
    --dir) [ $# -ge 2 ] || err "--dir needs a value"; INSTALL_DIR="$2"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --verbose) VERBOSE=1; shift ;;
    -h|--help)
      printf '%s\n' "Usage: sh install.sh [--version X.Y.Z] [--dir DIR] [--dry-run] [--verbose]" \
        "Verifies an Ed25519 signature and SHA-256 before installing pewter and its alias pew." \
        "Env: PEWTER_BASE_URL (https, or http on loopback)"
      exit 0 ;;
    *) err "unknown option: $1" ;;
  esac
done

# --- base URL: https, or http only on loopback ---
case "$BASE_URL" in
  https://*) ;;
  http://localhost:*|http://localhost/*|http://127.0.0.1:*|http://127.0.0.1/*|http://\[::1\]:*) ;;
  *) err "refusing base URL '$BASE_URL': use https (http is allowed only for localhost)" ;;
esac
BASE_URL="${BASE_URL%/}"

need uname; need tar; need mktemp; need openssl; need awk
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL --max-time 120 --proto '=https,http' -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -q -O "$2" "$1"; }
else
  err "need curl or wget"
fi
if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
  err "need sha256sum or shasum"
fi

# --- signature tool check: OpenSSL must support raw Ed25519 verification ---
openssl pkeyutl -help 2>&1 | grep -q rawin ||
  err "this openssl cannot verify Ed25519 signatures (macOS system LibreSSL is too old). Install with 'brew install omegacomputes/tap/pewter' (once published) or 'brew install openssl' and put it first on PATH."

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM

# --- public key ---
if [ -n "${PEWTER_RELEASE_PUBKEY_FILE:-}" ]; then
  [ -r "$PEWTER_RELEASE_PUBKEY_FILE" ] || err "cannot read PEWTER_RELEASE_PUBKEY_FILE"
  cp "$PEWTER_RELEASE_PUBKEY_FILE" "$TMP/pub.pem"
  warn "warning: using a public key from PEWTER_RELEASE_PUBKEY_FILE (testing only)"
else
  case "$RELEASE_PUBKEY" in
    *PLACEHOLDER*) err "no release key is configured in this copy of the installer, so nothing can be verified. Nothing was installed." ;;
  esac
  printf '%s\n' "$RELEASE_PUBKEY" > "$TMP/pub.pem"
fi
openssl pkey -pubin -in "$TMP/pub.pem" -noout >/dev/null 2>&1 || err "release public key is not a valid public key"

# verify_sig FILE: FILE.sig must be a valid Ed25519 signature of FILE
verify_sig() {
  openssl pkeyutl -verify -rawin -pubin -inkey "$TMP/pub.pem" -sigfile "$1.sig" -in "$1" >/dev/null 2>&1 ||
    err "signature check failed for $(basename "$1"). Nothing was installed."
}

# --- platform ---
case "$(uname -s)" in
  Linux) OS=linux ;;
  Darwin) OS=darwin ;;
  *) err "unsupported OS: $(uname -s)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) err "unsupported architecture: $(uname -m)" ;;
esac

# --- version (latest.txt is signed, so a mirror cannot roll you back) ---
if [ -z "$VERSION" ]; then
  info "Resolving latest version..."
  fetch "$BASE_URL/latest.txt" "$TMP/latest.txt" || err "could not download latest.txt"
  fetch "$BASE_URL/latest.txt.sig" "$TMP/latest.txt.sig" || err "could not download latest.txt.sig"
  verify_sig "$TMP/latest.txt"
  VERSION="$(tr -d '[:space:]' < "$TMP/latest.txt")"
fi
VERSION="${VERSION#v}"
case "$VERSION" in
  ''|*[!0-9A-Za-z.+-]*) err "invalid version string" ;;
esac

ARCHIVE="pewter-v$VERSION-$OS-$ARCH.tar.gz"
info "Installing pewter v$VERSION ($OS/$ARCH)"
fetch "$BASE_URL/$ARCHIVE" "$TMP/$ARCHIVE" || err "could not download $ARCHIVE"
fetch "$BASE_URL/SHA256SUMS" "$TMP/SHA256SUMS" || err "could not download SHA256SUMS"
fetch "$BASE_URL/SHA256SUMS.sig" "$TMP/SHA256SUMS.sig" || err "could not download SHA256SUMS.sig (unsigned releases are refused)"

info "Verifying signature..."
verify_sig "$TMP/SHA256SUMS"

info "Verifying checksum..."
WANT="$(awk -v f="$ARCHIVE" '$2 == f || $2 == "*" f {print $1; n++} END {if (n != 1) exit 1}' "$TMP/SHA256SUMS")" ||
  err "$ARCHIVE is not listed exactly once in SHA256SUMS. Nothing was installed."
GOT="$(sha256 "$TMP/$ARCHIVE")"
[ "$WANT" = "$GOT" ] || err "checksum mismatch for $ARCHIVE. Nothing was installed."

# --- archive must hold exactly three files: LICENSE, NOTICE and pewter ---
LIST="$(tar -tzf "$TMP/$ARCHIVE" | LC_ALL=C sort)" || err "cannot read archive"
EXPECTED="$(printf 'LICENSE\nNOTICE\n%s' "$BINARY")"
[ "$LIST" = "$EXPECTED" ] || err "unexpected archive contents. Nothing was installed."
mkdir "$TMP/x"
tar -xzf "$TMP/$ARCHIVE" -C "$TMP/x" LICENSE NOTICE "$BINARY"
for f in LICENSE NOTICE "$BINARY"; do
  [ -f "$TMP/x/$f" ] && [ ! -L "$TMP/x/$f" ] || err "archive entry $f is not a regular file. Nothing was installed."
done
chmod 0755 "$TMP/x/$BINARY"

if [ "$DRY_RUN" = 1 ]; then
  say "Dry run: pewter v$VERSION verified. Nothing was installed."
  exit 0
fi

if [ -z "$INSTALL_DIR" ]; then
  if [ "$(id -u)" -eq 0 ]; then INSTALL_DIR=/usr/local/bin; else INSTALL_DIR="$HOME/.local/bin"; fi
fi
mkdir -p "$INSTALL_DIR"
"$TMP/x/$BINARY" version >/dev/null 2>&1 || err "the verified binary does not run on this machine"
mv -f "$TMP/x/$BINARY" "$INSTALL_DIR/$BINARY.new" && mv -f "$INSTALL_DIR/$BINARY.new" "$INSTALL_DIR/$BINARY"

# Short alias `pew`: a symlink to pewter (same binary, identical behaviour).
# Never overwrite a different program that is already called pew.
ALIAS_NOTE=""
if [ -L "$INSTALL_DIR/$ALIAS" ] && [ "$(readlink "$INSTALL_DIR/$ALIAS")" = "$BINARY" ]; then
  :
elif [ -e "$INSTALL_DIR/$ALIAS" ] || [ -L "$INSTALL_DIR/$ALIAS" ]; then
  ALIAS_NOTE="Note: $INSTALL_DIR/$ALIAS already exists and is not ours, so it was left alone. Use pewter."
else
  ln -s "$BINARY" "$INSTALL_DIR/$ALIAS" 2>/dev/null || cp "$INSTALL_DIR/$BINARY" "$INSTALL_DIR/$ALIAS"
fi

say "Installed pewter v$VERSION (alias: pew) in $INSTALL_DIR"
say "Next:  pewter login"
say "Help:  pewter --help"
[ -z "$ALIAS_NOTE" ] || warn "$ALIAS_NOTE"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) warn "Add it to your PATH:  export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac
