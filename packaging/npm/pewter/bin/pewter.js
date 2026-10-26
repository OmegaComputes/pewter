#!/usr/bin/env node
// Launcher for the pewter binary. Used for both `pewter` and `pew`.
// No postinstall download: the binary ships inside a per-platform optional
// dependency (@omegacompute/pewter-<os>-<arch>) that npm picks by os/cpu.
"use strict";
const { spawnSync } = require("node:child_process");
const fs = require("node:fs");

const PLATFORMS = {
  "darwin-arm64": "@omegacompute/pewter-darwin-arm64",
  "darwin-x64": "@omegacompute/pewter-darwin-x64",
  "linux-arm64": "@omegacompute/pewter-linux-arm64",
  "linux-x64": "@omegacompute/pewter-linux-x64",
};

function resolveBinary(platform = process.platform, arch = process.arch, resolve = require.resolve) {
  const pkg = PLATFORMS[`${platform}-${arch}`];
  if (!pkg) {
    throw new Error(`pewter has no build for ${platform}/${arch}. Contact devs@h3ir.com.`);
  }
  try {
    return resolve(`${pkg}/bin/pewter`);
  } catch (_) {
    throw new Error(
      `The platform package ${pkg} is not installed (npm skipped optional dependencies?). ` +
        `Reinstall without --omit=optional, or use: curl -fsSL https://omegacompute.com/install | sh`
    );
  }
}

function main() {
  let bin;
  try {
    bin = resolveBinary();
    try { fs.accessSync(bin, fs.constants.X_OK); } catch (_) { fs.chmodSync(bin, 0o755); }
  } catch (e) {
    process.stderr.write(`pewter: ${e.message}\n`);
    process.exit(1);
  }
  const r = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
  if (r.error) {
    process.stderr.write(`pewter: ${r.error.message}\n`);
    process.exit(1);
  }
  if (r.signal) process.kill(process.pid, r.signal);
  process.exit(r.status === null ? 1 : r.status);
}

if (require.main === module) main();
module.exports = { resolveBinary, PLATFORMS };
