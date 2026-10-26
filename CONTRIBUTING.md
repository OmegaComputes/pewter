# Contributing

Thanks for looking at `pewter`, the command-line client for Pewter orchestration.

- Small, focused pull requests are easiest to review. Open an issue first for anything large.
- Run the checks before you push:

  ```sh
  gofmt -l . && go vet ./... && go test -race ./...
  bash tests/install_test.sh && bash tests/packaging_test.sh
  node --test packaging/npm/test/
  ```

- Keep the CLI standard-library only unless there is a strong reason not to.
- Tokens and secrets: never in argv, never printed, never in the config file. Tests use fake values only.
- Output stays plain: tables for people, `--json` for scripts, nothing decorative on stdout.
- Wording: say "orchestration".
- Security problems: do not open an issue. See [SECURITY.md](SECURITY.md) (devs@h3ir.com).

## License of contributions

This project is licensed under the [Apache License 2.0](LICENSE). Unless you state otherwise, any contribution you
intentionally submit for inclusion in this project is licensed under Apache-2.0, as described in section 5 of the license,
with no additional terms or conditions.
