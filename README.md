# pewter

Orchestration for GPU compute, from the terminal.

`pewter` is the command-line client for Pewter. The short alias `pew` does exactly the same thing; use either.
Everything the CLI prints is plain text for people, or JSON for scripts and agents (`--json`).

HEIR PTE. LTD., trading as Omega Compute · [omegacompute.com](https://omegacompute.com) · devs@h3ir.com

> **Status: v0.1.0: read-only, staging API, access by permissioned API key (pck_) approved in the Pewter app.**
> The signed release is not published yet. Until then, build from source. The install commands below are the ones that release will use.

## Install

On macOS:

```sh
brew install omegacomputes/tap/pewter
```

On Linux, and on macOS once OpenSSL 3 is first on `PATH`:

```sh
curl -fsSL https://omegacompute.com/install | sh
```

Prefer to read the installer first?

```sh
curl -fsSLo install.sh https://omegacompute.com/install && less install.sh && sh install.sh
```

The installer downloads a release, checks an Ed25519 signature on the checksum list and then the SHA-256 of the
archive, and installs only if both pass. It does not edit your shell profile and does not need root
(default directory `~/.local/bin`). It never overwrites a different program already named `pew`.
It supports macOS and Linux on amd64 and arm64.

Options: `--version X.Y.Z`, `--dir DIR`, `--dry-run` (verify only), `--verbose`.

npm is not part of v0.1.0. The names `pewter` and `pew` on the npm registry belong to other people. A later release may publish the scoped package `@omegacompute/pewter`. The scaffold under `packaging/npm/` stays private.

### Build from source

Needs Go 1.22 or newer.

```sh
git clone https://github.com/OmegaComputes/pewter && cd pewter
go build -o pewter ./cmd/pewter && ln -sf pewter pew
```

## Quick start

```sh
pewter login            # approve the code in the Pewter app, or: pewter login --with-token < key.txt
pewter whoami
pewter instruments --gpu H100
pewter orders
pewter --help
```

## Commands

| Command | What it does |
|---|---|
| `login` | Sign in. Approve the code in the Pewter app, or `--with-token` to read a `pck_` key from stdin |
| `logout` | Revoke the credential on the server, then remove the local token |
| `whoami` | Display name, credential id, scopes, expiry, sandbox flag, API host |
| `status` | Check that the API is reachable and print its host |
| `instruments` | List instruments (`--gpu`, `--term`, `--region`, `--limit`) |
| `listings` | Your listings (`--status`) |
| `orders` | Your orders (`--status`) |
| `balance` | Your balance |
| `verify <id>` | Validation status of one of your listings |
| `version` | Print the version (`--json` for details) |
| `completion bash\|zsh` | Print a shell completion script (registered for `pewter` and `pew`) |
| `rfq`, `buy`, `ask`, `sell`, `accept` | Unavailable in this release (exit code 2) |

## Authentication

A Pewter CLI key starts with `pck_`. It is created when you run `pewter login` and approve the code in the Pewter app. The CLI asks for the read scopes `deals:read`, `market:read` and `verification:read`. An agent key (`omk_`) and a session token (`pws_`) are refused and are not stored.

- **App approval:** `pewter login` prints a verification URL and a short code on stderr. Approve that code in the Pewter app. The token is never printed.
- **Existing key:** `pewter login --with-token < key.txt`. The key is read from stdin, never from the command line, so it does not end up in shell history or process listings. It is stored only after `GET /me` succeeds and the prefix is `pck_`.
- **CI and scripts:** set `PEWTER_TOKEN` to a `pck_` key. It overrides the stored token and is never written to disk. `pewter logout` revokes that credential on the server and tells you to unset the variable. It does not delete a different token that is stored on disk.
- Tokens are stored in the OS keychain (macOS Keychain, or Secret Service via `secret-tool` on Linux). If no keychain is available they fall back to a file with mode `0600` in a `0700` directory, and the CLI says so.
- `pewter logout` calls `POST /auth/credentials/self/revoke`, then deletes the local token. If the server says the credential is already revoked or expired (HTTP 401), the local token is still deleted.

## Scripting and agents

- `--json` prints a projection on stdout: `whoami`, `listings`, `orders`, `balance` and `verify` omit `principal_id`, `agent_id`, email, phone and company. Errors go to stderr, as JSON when `--json` is set.
- Nothing decorative is written to stdout.
- Exit codes: `0` ok, `1` error, `2` usage error or unavailable command, `3` not signed in, `4` network error.
- Use `--profile NAME` (or `PEWTER_PROFILE`) to keep separate credentials.

| Environment variable | Meaning |
|---|---|
| `PEWTER_TOKEN` | A `pck_` key for CI and scripts; overrides the stored token and is never written |
| `PEWTER_API_URL` | API base URL (also `--api-url`). v0.1.0 allows the staging host below, or loopback |
| `PEWTER_PROFILE` | Credential profile name (default `default`) |
| `PEWTER_KEYSTORE` | `auto`, `keychain` or `file` |
| `PEWTER_CONFIG_DIR` | Directory for non-secret settings (default `~/.config/pewter`) |

## API host

v0.1.0 talks only to the Pewter staging API:

`https://pewter-staging-production.up.railway.app/api/v1`

That host is the staging orchestration. Account data there is not the live book. `pewter status` prints the host. Loopback (`localhost`, `127.0.0.1`, `::1`) is allowed for tests. Any other host, including `https://api.omegacompute.com/api/v1`, is a hard error (exit 2). Production is not enabled in this release.

## Security model

- HTTPS only, except plain http to loopback. Redirects are not followed. Response size is capped.
- Text that comes from the server (titles, names) is stripped of terminal escape sequences before it is printed.
- The config file never holds a secret. The CLI collects no telemetry and does not send a token to a host outside the allow-list.
- The installer fails closed: a missing signature, a wrong key, a tampered archive or an archive with unexpected contents
  all install nothing.
- To report a vulnerability, see [SECURITY.md](SECURITY.md).

## Verify a release by hand

Each release publishes `pewter-vX.Y.Z-<os>-<arch>.tar.gz`, `SHA256SUMS` and `SHA256SUMS.sig`
(a raw Ed25519 signature) under `https://omegacompute.com/cli/`. Each archive holds exactly three files:
`pewter`, `LICENSE` and `NOTICE`. The installer refuses an archive with anything else in it. The release public key is embedded in `scripts/install.sh` and repeated here so you can compare the two. The private half is not in this repository.

```
-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAAmYZe6f/i1MIjZjFmG5kj25sS6FnUoGpOCkGw2Z88TY=
-----END PUBLIC KEY-----
```

```sh
openssl pkeyutl -verify -rawin -pubin -inkey release-key.pem -sigfile SHA256SUMS.sig -in SHA256SUMS
sha256sum -c --ignore-missing SHA256SUMS      # on macOS: shasum -a 256 -c --ignore-missing SHA256SUMS
```

OpenSSL 3 is needed for Ed25519 with `-rawin`. macOS ships an older LibreSSL; install OpenSSL with Homebrew
(`brew install openssl`) or install the CLI with `brew install omegacomputes/tap/pewter`.

## Development

```sh
go vet ./... && go test -race ./...
bash tests/install_test.sh          # builds a throwaway signed release and serves it on loopback
bash tests/packaging_test.sh        # Homebrew template and npm package checks
node --test packaging/npm/test/     # npm launcher tests (Node 18+)
```

Layout: `cmd/pewter` (the CLI), `cmd/pewter-mock` (a loopback-only fake API for local trials and tests),
`internal/` (client, keystore, config, rendering), `scripts/` (installer, release build),
`packaging/` (Homebrew formula template, npm package scaffold).

Try it against the local mock, without an account:

```sh
go run ./cmd/pewter-mock &                       # listens on 127.0.0.1:8799
export PEWTER_API_URL=http://127.0.0.1:8799/api/v1 PEWTER_KEYSTORE=file
go run ./cmd/pewter login && go run ./cmd/pewter orders
```

With no `PEWTER_API_URL` and no saved config, the binary uses the staging API above. `pewter instruments` needs no key. `pewter whoami` without a key exits 3.

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Copyright 2026 Heir Pte Ltd. Licensed under the [Apache License, Version 2.0](LICENSE). See [NOTICE](NOTICE).
