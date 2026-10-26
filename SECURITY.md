# Security

## Reporting a vulnerability

Email **devs@h3ir.com** with the subject `pewter security`. Please include what you found, the version
(`pewter version --json`), and steps to reproduce. Do not open a public issue for a vulnerability.

We will acknowledge your report and keep you updated. Please give us a reasonable time to fix the problem
before you disclose it.

## What is in scope

The `pewter` / `pew` command-line tool, its installer (`scripts/install.sh`), and the packaging in this repository:

- credential handling (keychain, file fallback, anything that could leak a token),
- the installer's signature and checksum verification,
- handling of untrusted text from the server,
- the Homebrew formula template and the npm launcher.

## Handling secrets

Never paste a real token or key into an issue, a pull request, a log or a test. Tests use made-up values only.
If you think a token was exposed, sign out (`pewter logout`) and revoke it in your account.

Heir Pte Ltd · omegacompute.com
