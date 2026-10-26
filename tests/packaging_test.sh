#!/bin/bash
# Checks the packaging templates (no publishing, no network).
set -u
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
P=0; F=0
ok(){ P=$((P+1)); echo "ok   - $1"; }; bad(){ F=$((F+1)); echo "FAIL - $1"; }
W="$(mktemp -d)"; trap 'rm -rf "$W"' EXIT

# template is clearly placeholder, never a real hash
n="$(grep -c 'sha256 "REPLACE_WITH_REAL_SHA256' "$ROOT/packaging/homebrew/pewter.rb")"
[ "$n" = 4 ] && ok "formula template has 4 clearly marked placeholder hashes" || bad "placeholders: $n"
grep -q 'install_symlink "pewter" => "pew"' "$ROOT/packaging/homebrew/pewter.rb" && ok "formula installs pew alias" || bad "formula alias"

# every Homebrew reference uses the tap name omegacomputes/tap/pewter (never a bare `brew install pewter`)
if grep -rnE 'brew install (<owner>/tap/)?pewter' "$ROOT" --include='*' --exclude-dir=.git --exclude-dir=node_modules --exclude=packaging_test.sh | grep -q .; then bad "stale Homebrew reference"; else ok "no stale Homebrew install references"; fi
grep -q 'brew install omegacomputes/tap/pewter' "$ROOT/packaging/homebrew/pewter.rb" && grep -q 'brew install omegacomputes/tap/pewter' "$ROOT/README.md" && ok "formula and README use omegacomputes/tap/pewter" || bad "tap name missing"
grep -q 'doc.install "LICENSE", "NOTICE"' "$ROOT/packaging/homebrew/pewter.rb" && ok "formula installs LICENSE and NOTICE" || bad "formula license install"

# update-formula refuses without the verified flag, fills hashes with it
for p in darwin-arm64 darwin-amd64 linux-arm64 linux-amd64; do printf '%064d  pewter-v1.2.3-%s.tar.gz\n' "$RANDOM" "$p"; done > "$W/SHA256SUMS"
"$ROOT/packaging/homebrew/update-formula.sh" 1.2.3 "$W/SHA256SUMS" >/dev/null 2>&1 && bad "update-formula ran without verification flag" || ok "update-formula refuses without --i-verified-the-signature"
out="$("$ROOT/packaging/homebrew/update-formula.sh" 1.2.3 "$W/SHA256SUMS" --i-verified-the-signature)"
echo "$out" | grep -q 'sha256 "REPLACE' && bad "placeholders left after fill" || ok "update-formula fills all hashes"
echo "$out" | grep -q 'TEMPLATE' && bad "template header leaked into filled formula" || ok "filled formula has no template header"
echo "$out" | grep -q 'version "1.2.3"' && ok "update-formula sets version" || bad "version not set"
head -n3 "$W/SHA256SUMS" > "$W/short"
"$ROOT/packaging/homebrew/update-formula.sh" 1.2.3 "$W/short" --i-verified-the-signature >/dev/null 2>&1 && bad "accepted incomplete SHA256SUMS" || ok "update-formula refuses incomplete SHA256SUMS"

# npm: pack list is just launcher + readme + package.json
files="$(cd "$ROOT/packaging/npm/pewter" && npm pack --dry-run --json 2>/dev/null | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>console.log(JSON.parse(s)[0].files.map(f=>f.path).sort().join(",")))')"
[ "$files" = "LICENSE,README.md,bin/pewter.js,package.json" ] && ok "npm package contains only launcher, README, LICENSE, package.json" || bad "npm files: $files"

echo; echo "packaging tests: $P passed, $F failed"; [ "$F" -eq 0 ]
