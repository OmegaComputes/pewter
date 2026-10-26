#!/bin/bash
# Local test of scripts/install.sh. Builds a throwaway signed release, serves it
# from 127.0.0.1, and checks that good input installs and every bad input
# installs nothing. Publishes, uploads and deploys nothing.
set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
W="$(mktemp -d)"; PIDS=""
cleanup() { for p in $PIDS; do kill "$p" 2>/dev/null; done; rm -rf "$W"; }
trap cleanup EXIT
PASS=0; FAIL=0
ok()   { PASS=$((PASS+1)); echo "ok   - $1"; }
bad()  { FAIL=$((FAIL+1)); echo "FAIL - $1"; }

# throwaway keys (never reused, never committed)
openssl genpkey -algorithm ed25519 -out "$W/test.key" 2>/dev/null
openssl pkey -in "$W/test.key" -pubout -out "$W/test.pub" 2>/dev/null
openssl genpkey -algorithm ed25519 -out "$W/other.key" 2>/dev/null

HOSTOS="$(uname -s | tr A-Z a-z)"; case "$(uname -m)" in x86_64) HA=amd64;; aarch64|arm64) HA=arm64;; esac
V=9.9.9
mkrel() { # dir
  PEWTER_TARGETS="$HOSTOS/$HA" PEWTER_SIGNING_KEY="$W/test.key" "$ROOT/scripts/build-release.sh" "$V" "$1" >/dev/null 2>&1
}
mkrel "$W/good" || { echo "build failed"; exit 1; }

PORT=$((20000 + RANDOM % 20000))
serve() { # dir port
  ( cd "$1" && exec python3 -m http.server "$2" --bind 127.0.0.1 >/dev/null 2>&1 ) & PIDS="$PIDS $!"
}
serve "$W/good" "$PORT"
for _ in $(seq 1 50); do curl -fsS "http://127.0.0.1:$PORT/latest.txt" >/dev/null 2>&1 && break; sleep 0.1; done

inst() { # name base [extra args...]  -> runs installer into $W/inst-$name
  local name="$1" base="$2"; shift 2
  rm -rf "$W/inst-$name"
  PEWTER_BASE_URL="$base" PEWTER_RELEASE_PUBKEY_FILE="${KEYFILE:-$W/test.pub}" sh "$ROOT/scripts/install.sh" --dir "$W/inst-$name" "$@" >"$W/out-$name" 2>&1
}
installed() { [ -e "$W/inst-$1/pewter" ]; }
nothing()   { ! installed "$1"; }
base="http://127.0.0.1:$PORT"

# 1 good install (latest.txt path, signed)
if inst good "$base" && installed good; then
  v="$("$W/inst-good/pewter" version --json | tr -d ' \n')"
  case "$v" in *'"version":"9.9.9"'*) ok "signed release installs and reports 9.9.9";; *) bad "version wrong: $v";; esac
else bad "good install failed: $(cat "$W/out-good")"; fi

# 1b alias + clean 3-line success message
if [ -L "$W/inst-good/pew" ] && [ "$(readlink "$W/inst-good/pew")" = pewter ] && [ "$("$W/inst-good/pew" version --json | tr -d ' \n')" = "$("$W/inst-good/pewter" version --json | tr -d ' \n')" ]; then ok "pew alias installed (symlink to pewter, same output)"; else bad "pew alias"; fi
grep -v '^Add it to your PATH\|^warning: using a public key' "$W/out-good" > "$W/msg-good"
lines="$(wc -l < "$W/msg-good" | tr -d ' ')"
if [ "$lines" = 3 ] && sed -n 1p "$W/msg-good" | grep -q '^Installed pewter v9.9.9 (alias: pew)' && sed -n 2p "$W/msg-good" | grep -q 'pewter login' && sed -n 3p "$W/msg-good" | grep -q 'pewter --help'; then ok "success message is exactly: version, pewter login, pewter --help"; else bad "success message ($lines lines): $(cat "$W/out-good")"; fi

# 1c re-running over an existing install is fine; a foreign `pew` is never overwritten
PEWTER_BASE_URL="$base" PEWTER_RELEASE_PUBKEY_FILE="$W/test.pub" sh "$ROOT/scripts/install.sh" --dir "$W/inst-good" >/dev/null 2>&1 && [ -L "$W/inst-good/pew" ] && ok "re-install over existing install works" || bad "re-install"
mkdir -p "$W/inst-foreign"; printf '#!/bin/sh\necho foreign\n' > "$W/inst-foreign/pew"; chmod +x "$W/inst-foreign/pew"
PEWTER_BASE_URL="$base" PEWTER_RELEASE_PUBKEY_FILE="$W/test.pub" sh "$ROOT/scripts/install.sh" --dir "$W/inst-foreign" >"$W/out-foreign" 2>&1
if [ "$("$W/inst-foreign/pew")" = foreign ] && [ -x "$W/inst-foreign/pewter" ] && grep -q "already exists and is not ours" "$W/out-foreign"; then ok "existing foreign 'pew' left alone with a note"; else bad "foreign pew: $(cat "$W/out-foreign")"; fi

# 2 pinned version
inst pinned "$base" --version "$V" && installed pinned && ok "--version pin installs" || bad "pinned install"

# 3 dry run installs nothing
if inst dry "$base" --dry-run && nothing dry && [ ! -e "$W/inst-dry/pew" ] && grep -q "Dry run" "$W/out-dry"; then ok "--dry-run verifies, installs nothing"; else bad "dry run"; fi

# helper: copy good release, mutate, serve, expect failure + message + nothing installed
mut() { # name expected-message mutate-cmd...
  local name="$1" msg="$2"; shift 2
  cp -r "$W/good" "$W/m-$name"
  ( cd "$W/m-$name" && "$@" ) >/dev/null 2>&1
  local p=$((PORT + 1 + RANDOM % 9000)); serve "$W/m-$name" "$p"
  for _ in $(seq 1 50); do curl -fsS "http://127.0.0.1:$p/SHA256SUMS" >/dev/null 2>&1 && break; sleep 0.1; done
  if inst "$name" "http://127.0.0.1:$p" --version "$V"; then bad "$name: installer succeeded but should fail"
  elif ! nothing "$name" || [ -e "$W/inst-$name/pew" ]; then bad "$name: failed but still installed a file"
  elif ! grep -q "$msg" "$W/out-$name"; then bad "$name: wrong message: $(tail -1 "$W/out-$name")"
  else ok "$name -> refused ('$msg')"; fi
}
A="pewter-v$V-$HOSTOS-$HA.tar.gz"
mut tampered-archive "checksum mismatch" sh -c "printf x >> $A"
mut tampered-sums "signature check failed" sh -c "sed -i 's/^./0/' SHA256SUMS"
mut missing-sig "SHA256SUMS.sig" rm -f SHA256SUMS.sig
mut wrong-key "signature check failed" sh -c "openssl pkeyutl -sign -rawin -inkey $W/other.key -in SHA256SUMS -out SHA256SUMS.sig"
mut archive-unlisted "not listed exactly once" sh -c "echo '0000 other.tar.gz' > SHA256SUMS && openssl pkeyutl -sign -rawin -inkey $W/test.key -in SHA256SUMS -out SHA256SUMS.sig"
# correctly signed but hostile archive (extra file + traversal name): signature and checksum pass, content check must stop it
mut evil-archive "unexpected archive contents" sh -c "mkdir e && echo hi > e/pewter && echo x > e/extra && tar -C e -czf $A pewter extra && sha256sum pewter-v$V-*.tar.gz > SHA256SUMS && openssl pkeyutl -sign -rawin -inkey $W/test.key -in SHA256SUMS -out SHA256SUMS.sig"

# latest.txt rollback / tamper
cp -r "$W/good" "$W/m-latest"; echo "0.0.1" > "$W/m-latest/latest.txt"
p=$((PORT + 9500)); serve "$W/m-latest" "$p"; for _ in $(seq 1 50); do curl -fsS "http://127.0.0.1:$p/latest.txt" >/dev/null 2>&1 && break; sleep 0.1; done
if inst latest "http://127.0.0.1:$p" ; then bad "tampered latest.txt accepted"; elif nothing latest && grep -q "signature check failed" "$W/out-latest"; then ok "tampered latest.txt -> refused"; else bad "latest: $(tail -1 "$W/out-latest")"; fi

# placeholder key, no override: refuse
rm -rf "$W/inst-ph"
PEWTER_BASE_URL="$base" sh "$ROOT/scripts/install.sh" --dir "$W/inst-ph" >"$W/out-ph" 2>&1
if [ $? -ne 0 ] && [ ! -e "$W/inst-ph/pewter" ] && grep -q "no release key is configured" "$W/out-ph"; then ok "placeholder key -> refuses to install"; else bad "placeholder key: $(cat "$W/out-ph")"; fi

# non-loopback http, and weird schemes
for u in "http://example.com/cli" "ftp://x/y" "file:///tmp"; do
  rm -rf "$W/inst-u"; PEWTER_BASE_URL="$u" PEWTER_RELEASE_PUBKEY_FILE="$W/test.pub" sh "$ROOT/scripts/install.sh" --dir "$W/inst-u" >"$W/out-u" 2>&1
  if [ $? -ne 0 ] && grep -q "refusing base URL" "$W/out-u"; then ok "base URL '$u' refused"; else bad "base URL $u accepted"; fi
done

# bad version strings
for vv in '../../x' '1.0;rm' ''; do
  [ -z "$vv" ] && continue
  if inst badv "$base" --version "$vv"; then bad "version '$vv' accepted"; else ok "version '$vv' refused"; fi
done

# the installer must not touch shell profiles
if [ ! -e "$W/.zshrc" ] && ! grep -q "zshrc\|bashrc\|bash_profile" "$ROOT/scripts/install.sh"; then ok "installer does not edit shell profiles"; else bad "profile edit"; fi

echo; echo "install tests: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
