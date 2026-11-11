# Releasing pewter

Only maintainers at Heir Pte Ltd do this. Nothing here runs automatically: every step that publishes
something is done by hand. OpenSSL 3 is required for the signing steps (`openssl version`).

The trust model: users verify an Ed25519 signature on `SHA256SUMS` (and on `latest.txt`) with a public key built
into `scripts/install.sh`. The private half signs releases and **never touches a build machine, a CI runner or
this repository.**

## Release signing key

The Ed25519 key was generated on the Mac Studio. The private key and its passphrase stay outside git: not on a build machine, not in CI, and not in this repository. The public half is in `scripts/install.sh` (`RELEASE_PUBKEY`) and in the README, so a reader can compare them. `tests/install_test.sh` checks that the embedded key is a valid Ed25519 public key. It signs with a throwaway key via `PEWTER_RELEASE_PUBKEY_FILE`, not with the release key.

Losing the private key means shipping a new public key in a new installer. Leaking it means rotating (see the bottom of this file).

To rotate, generate a new key outside git and replace the public half:

```sh
openssl genpkey -algorithm ed25519 | openssl pkcs8 -topk8 -v2 aes-256-cbc -out pewter-release.key   # asks for a passphrase
chmod 600 pewter-release.key
openssl pkey -in pewter-release.key -pubout -out pewter-release.pub
```

Paste the whole `-----BEGIN PUBLIC KEY-----` block into `scripts/install.sh` and the README. If the text `PLACEHOLDER` is put back, the installer refuses to install anything.

## Every release

1. On the build machine, from a clean checkout of the release commit, with tests green. macOS needs GNU tar (`brew install gnu-tar`, which provides `gtar`; the script uses `gtar` when `tar` is bsdtar):
   ```sh
   go test ./... && bash tests/install_test.sh && bash tests/packaging_test.sh
   scripts/build-release.sh X.Y.Z dist          # no PEWTER_SIGNING_KEY here, on purpose
   ```
   This writes `dist/pewter-vX.Y.Z-<os>-<arch>.tar.gz` for linux and darwin on amd64 and arm64, plus `SHA256SUMS`
   and `latest.txt`. Each archive holds exactly `LICENSE`, `NOTICE` and `pewter`. The script says the output is
   not signed. That is expected.
2. Carry `SHA256SUMS` and `latest.txt` (only those two small files) to the machine that holds the private key. Look at them: four
   archives listed, the version in `latest.txt` is `X.Y.Z`. Then sign. The key is encrypted, so pass the passphrase with `-passin` (the same value `scripts/build-release.sh` reads from `PEWTER_SIGNING_PASSIN` when `PEWTER_SIGNING_KEY` is set):
   ```sh
   export PEWTER_SIGNING_PASSIN='pass:env:PEWTER_KEY_PASSPHRASE'   # or file:/path/to/passphrase
   openssl pkeyutl -sign -rawin -inkey pewter-release.key -passin "$PEWTER_SIGNING_PASSIN" -in SHA256SUMS -out SHA256SUMS.sig
   openssl pkeyutl -sign -rawin -inkey pewter-release.key -passin "$PEWTER_SIGNING_PASSIN" -in latest.txt -out latest.txt.sig
   openssl pkeyutl -verify -rawin -pubin -inkey pewter-release.pub -sigfile SHA256SUMS.sig -in SHA256SUMS
   openssl pkeyutl -verify -rawin -pubin -inkey pewter-release.pub -sigfile latest.txt.sig -in latest.txt
   ```
   `scripts/build-release.sh` uses `${OPENSSL:-openssl}` and, when `PEWTER_SIGNING_PASSIN` is set, passes `-passin "$PEWTER_SIGNING_PASSIN"` to `openssl pkeyutl`. Both verifies must print `Signature Verified Successfully`.
3. Bring the two `.sig` files back to `dist/` next to the archives.
4. Before uploading, dry-run the installer against the local files:
   ```sh
   cd dist && python3 -m http.server 8099 --bind 127.0.0.1 &
   PEWTER_BASE_URL=http://127.0.0.1:8099 sh ../scripts/install.sh --dry-run --verbose
   ```
   It must end with "Dry run: pewter vX.Y.Z verified. Nothing was installed."

## macOS notarization (deferred for 0.1.0)

Apple notarization is deferred for 0.1.0. The darwin binaries in this release are not notarized. A browser download of a GitHub Release asset sets the quarantine attribute, and Gatekeeper can block an unnotarized binary. Homebrew installs are unaffected by Gatekeeper quarantine. `curl | sh` with the system `curl` usually does not set quarantine either. The supported macOS install for 0.1.0 is:

```sh
brew install omegacomputes/tap/pewter
```

Do not describe the GitHub macOS asset as Gatekeeper-clean.

## Go-live checklist (first release)

- [x] Signing key generated on the Mac Studio and kept outside git, passphrase stored separately
- [x] Public key embedded in `scripts/install.sh` and the README
- [ ] Public key also published on omegacompute.com (when that upload is approved)
- [ ] First release built, `SHA256SUMS` and `latest.txt` signed, both signatures verified
- [ ] Installer dry-run passes locally against the signed files
- [ ] Archives, `SHA256SUMS`, `SHA256SUMS.sig`, `latest.txt`, `latest.txt.sig` uploaded to `https://omegacompute.com/cli/`
- [ ] `scripts/install.sh` served at `https://omegacompute.com/install`
- [ ] On a clean Linux box: `curl -fsSL https://omegacompute.com/install | sh`, then `pewter version`, `pew version`
- [ ] On a Mac: `brew install omegacomputes/tap/pewter` (darwin binaries are not notarized; see above)
- [ ] Staging API answers, and `pewter login` works against `https://pewter-staging-production.up.railway.app/api/v1`. Production is not enabled in v0.1.0
- [ ] Git tag `vX.Y.Z` and a GitHub release created, with the same files attached, noting that darwin binaries are not notarized
- [ ] Homebrew: the tap `OmegaComputes/homebrew-tap` already exists. Generate `Formula/pewter.rb` with
      `packaging/homebrew/update-formula.sh X.Y.Z dist/SHA256SUMS --i-verified-the-signature` (the file in this repo stays a template), run
      `brew audit --strict --new --online omegacomputes/tap/pewter` and `brew install omegacomputes/tap/pewter` on a Mac, then push the formula to that tap
- [ ] npm is not part of v0.1.0
- [ ] README status banner updated once the install commands are live
- [ ] Run `gitleaks` over the repository one last time before the tag

## If the private key leaks or is lost

Stop serving the old files. Generate a new key offline, replace the public key in `scripts/install.sh`,
re-sign the current release, publish the new installer, and tell users to re-run it. Old installers carry
the old key and will refuse files signed with the new one. That is the intended failure mode.
