# Releasing pewter

Only maintainers at Heir Pte Ltd do this. Nothing here runs automatically: every step that publishes
something is done by hand. OpenSSL 3 is required for the signing steps (`openssl version`).

The trust model: users verify an Ed25519 signature on `SHA256SUMS` (and on `latest.txt`) with a public key built
into `scripts/install.sh`. The private half signs releases and **never touches a build machine, a CI runner or
this repository.**

## One-time: create the release signing key (offline)

Do this on an offline, trusted machine (not the machine that builds releases).

```sh
openssl genpkey -algorithm ed25519 | openssl pkcs8 -topk8 -v2 aes-256-cbc -out pewter-release.key   # asks for a passphrase
chmod 600 pewter-release.key
openssl pkey -in pewter-release.key -pubout -out pewter-release.pub                                   # the public half
cat pewter-release.pub
```

1. Store `pewter-release.key` and its passphrase in two separate offline places (for example, an encrypted USB
   stick in a safe, and a password manager entry for the passphrase). Losing the key means shipping a new
   public key in a new installer. Leaking it means rotating (see the bottom of this file).
2. Paste the **public** half (the whole `-----BEGIN PUBLIC KEY-----` block from `pewter-release.pub`) into
   `scripts/install.sh`, replacing the placeholder between the quotes of `RELEASE_PUBKEY`. While the text
   `PLACEHOLDER` is there, the installer refuses to install anything.
3. Check it and commit it:
   ```sh
   openssl pkey -pubin -in pewter-release.pub -noout && echo "public key parses"
   bash tests/install_test.sh      # with a real key embedded, one test checks it is a valid Ed25519 public key
   ```
   The placeholder-refusal test only runs while the placeholder is still in `scripts/install.sh`.
4. Publish the public key at a stable address on omegacompute.com as well, so people can check what the
   installer embeds (the README promises this).

## Every release

1. On the build machine, from a clean checkout of the release commit, with tests green:
   ```sh
   go test ./... && bash tests/install_test.sh && bash tests/packaging_test.sh
   scripts/build-release.sh X.Y.Z dist          # no PEWTER_SIGNING_KEY here, on purpose
   ```
   This writes `dist/pewter-vX.Y.Z-<os>-<arch>.tar.gz` for linux and darwin on amd64 and arm64, plus `SHA256SUMS`
   and `latest.txt`. Each archive holds exactly `LICENSE`, `NOTICE` and `pewter`. The script says the output is
   not signed. That is expected.
2. Carry `SHA256SUMS` and `latest.txt` (only those two small files) to the offline machine. Look at them: four
   archives listed, the version in `latest.txt` is `X.Y.Z`. Then sign:
   ```sh
   openssl pkeyutl -sign -rawin -inkey pewter-release.key -in SHA256SUMS -out SHA256SUMS.sig
   openssl pkeyutl -sign -rawin -inkey pewter-release.key -in latest.txt -out latest.txt.sig
   openssl pkeyutl -verify -rawin -pubin -inkey pewter-release.pub -sigfile SHA256SUMS.sig -in SHA256SUMS
   openssl pkeyutl -verify -rawin -pubin -inkey pewter-release.pub -sigfile latest.txt.sig -in latest.txt
   ```
   Both verifies must print `Signature Verified Successfully`.
3. Bring the two `.sig` files back to `dist/` next to the archives.
4. Before uploading, dry-run the installer against the local files:
   ```sh
   cd dist && python3 -m http.server 8099 --bind 127.0.0.1 &
   PEWTER_BASE_URL=http://127.0.0.1:8099 sh ../scripts/install.sh --dry-run --verbose
   ```
   It must end with "Dry run: pewter vX.Y.Z verified. Nothing was installed."

## Go-live checklist (first release)

- [ ] Signing key generated offline, backed up in two places, passphrase stored separately
- [ ] Public key pasted into `scripts/install.sh` (placeholder block gone), committed, tests green
- [ ] Public key published on omegacompute.com
- [ ] First release built, `SHA256SUMS` and `latest.txt` signed offline, both signatures verified
- [ ] Installer dry-run passes locally against the signed files
- [ ] Archives, `SHA256SUMS`, `SHA256SUMS.sig`, `latest.txt`, `latest.txt.sig` uploaded to `https://omegacompute.com/cli/`
- [ ] `scripts/install.sh` served at `https://omegacompute.com/install`
- [ ] On a clean Linux box and a clean Mac (with OpenSSL 3): `curl -fsSL https://omegacompute.com/install | sh`, then `pewter version`, `pew version`
- [ ] `api.omegacompute.com` is up, and `pewter login` works against it
- [ ] Git tag `vX.Y.Z` and a GitHub release created, with the same files attached
- [ ] Homebrew: create the public repo `OmegaComputes/homebrew-tap`, generate `Formula/pewter.rb` with
      `packaging/homebrew/update-formula.sh X.Y.Z dist/SHA256SUMS --i-verified-the-signature`, run
      `brew audit --strict --new --online omegacomputes/tap/pewter` and `brew install omegacomputes/tap/pewter` on a Mac, then push
- [ ] npm (optional): check the `@omegacompute` scope is ours, build platform packages with
      `node packaging/npm/scripts/build-platform-packages.js X.Y.Z dist out`, set `private` to false only when ready to publish
- [ ] README status banner updated (the install commands are now live)
- [ ] Run `gitleaks` over the repository one last time before the tag

## If the private key leaks or is lost

Stop serving the old files. Generate a new key offline, replace the public key in `scripts/install.sh`,
re-sign the current release, publish the new installer, and tell users to re-run it. Old installers carry
the old key and will refuse files signed with the new one. That is the intended failure mode.
