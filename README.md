# pewter

Orchestration for GPU compute, from the terminal.

`pewter` is the command-line client for Pewter. The short alias `pew` does exactly the same thing; use either.
Everything the CLI prints is plain text for people, or JSON for scripts and agents (`--json`).

Heir Pte Ltd · [omegacompute.com](https://omegacompute.com) · devs@h3ir.com

> **Status: early.** Read-only commands only. The first signed release has not been published yet, so the install
> commands below go live with it. Until then you can build from source (see below).

## Install

```sh
curl -fsSL https://omegacompute.com/install | sh      # installs pewter and pew
brew install omegacomputes/tap/pewter                 # Homebrew, from our own tap
npm i -g @omegacompute/pewter                         # npm wrapper, provides pewter and pew
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

The npm names `pewter` and `pew` belong to other people. Use the scoped package `@omegacompute/pewter`.

### Build from source

Needs Go 1.22 or newer.

```sh
git clone https://github.com/OmegaComputes/pewter && cd pewter
go build -o pewter ./cmd/pewter && ln -sf pewter pew
```

## Quick start

```sh
pewter login            # sign in (browser) or: pewter login --with-token < key.txt
pewter whoami
pewter instruments --gpu H100
pewter orders
pewter --help
```

## Commands

| Command | What it does |
|---|---|
| `login` | Sign in with a device code in your browser, or `--with-token` to read an API key from stdin |
| `logout` | Remove the stored token from this machine |
| `whoami` | Show who you are signed in as |
| `status` | Check that the API is reachable |
| `instruments` | List instruments (`--gpu`, `--term`, `--region`, `--limit`) |
| `listings` | Your listings (`--status`) |
| `orders` | Your orders (`--status`) |
| `balance` | Your balance |
| `verify <id>` | Validation status of one of your listings |
| `version` | Print the version (`--json` for details) |
| `completion bash\|zsh` | Print a shell completion script (registered for `pewter` and `pew`) |
| `rfq`, `buy`, `ask`, `sell`, `accept` | Unavailable in this release (exit code 2) |

## Authentication

- **Browser sign-in:** `pewter login` shows a short code and a link; you approve it in your browser. If the server
  does not offer it, the CLI tells you to use an API key instead.
- **API key:** `pewter login --with-token < key.txt`. The key is read from stdin, never from the command line, so it
  does not end up in shell history or process listings.
- **CI and scripts:** set `PEWTER_TOKEN`. It overrides the stored token and is never written to disk.
- Tokens are stored in the OS keychain (macOS Keychain, or Secret Service via `secret-tool` on Linux). If no keychain
  is available they fall back to a file with mode `0600` in a `0700` directory, and the CLI says so.
  Tokens are never printed.
- `pewter logout` removes the local token.

## Scripting and agents

- `--json` prints the API response data as JSON on stdout. Errors go to stderr, as JSON when `--json` is set.
- Nothing decorative is written to stdout.
- Exit codes: `0` ok, `1` error, `2` usage error or unavailable command, `3` not signed in, `4` network error.
- Use `--profile NAME` (or `PEWTER_PROFILE`) to keep separate credentials.

| Environment variable | Meaning |
|---|---|
| `PEWTER_TOKEN` | API key for CI and scripts; overrides the stored token |
| `PEWTER_API_URL` | API base URL (also `--api-url`); https only, http allowed for localhost |
| `PEWTER_PROFILE` | Credential profile name (default `default`) |
| `PEWTER_KEYSTORE` | `auto`, `keychain` or `file` |
| `PEWTER_CONFIG_DIR` | Directory for non-secret settings (default `~/.config/pewter`) |

## Security model

- HTTPS only (plain http only for localhost). Redirects are not followed. Response size is capped.
- Text that comes from the server (titles, names) is stripped of terminal escape sequences before it is printed.
- The config file never holds a secret. The CLI collects no telemetry and talks only to the API base you configure.
- The installer fails closed: a missing signature, a wrong key, a tampered archive or an archive with unexpected contents
  all install nothing.
- To report a vulnerability, see [SECURITY.md](SECURITY.md).

## Verify a release by hand

Each release publishes `pewter-vX.Y.Z-<os>-<arch>.tar.gz`, `SHA256SUMS` and `SHA256SUMS.sig`
(a raw Ed25519 signature) under `https://omegacompute.com/cli/`. Each archive holds exactly three files:
`pewter`, `LICENSE` and `NOTICE`. The installer refuses an archive with anything else in it. The release public key is published at
omegacompute.com and is also embedded in `scripts/install.sh`.

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

Try it without any account:

```sh
go run ./cmd/pewter-mock &                       # listens on 127.0.0.1:8799
export PEWTER_API_URL=http://127.0.0.1:8799/api/v1 PEWTER_KEYSTORE=file
go run ./cmd/pewter login && go run ./cmd/pewter orders
```

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Copyright 2026 Heir Pte Ltd. Licensed under the [Apache License, Version 2.0](LICENSE). See [NOTICE](NOTICE).
