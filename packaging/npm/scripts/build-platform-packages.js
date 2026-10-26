#!/usr/bin/env node
// Build the per-platform npm packages from a local release dir. LOCAL ONLY: never publishes.
//   node build-platform-packages.js <version> <dist-dir> <out-dir>
// Checks each archive against SHA256SUMS first. (The signature on SHA256SUMS must be verified separately.)
"use strict";
const fs = require("node:fs");
const path = require("node:path");
const crypto = require("node:crypto");
const { execFileSync } = require("node:child_process");

const MAP = [
  ["darwin", "arm64", "darwin", "arm64"],
  ["darwin", "amd64", "darwin", "x64"],
  ["linux", "arm64", "linux", "arm64"],
  ["linux", "amd64", "linux", "x64"],
];

function build(version, dist, out) {
  if (!/^[0-9A-Za-z.+-]+$/.test(version)) throw new Error("bad version");
  const sums = new Map(
    fs.readFileSync(path.join(dist, "SHA256SUMS"), "utf8").split("\n").filter(Boolean)
      .map((l) => { const [h, f] = l.split(/\s+\*?/); return [f, h]; })
  );
  const made = [];
  for (const [gos, goarch, nos, ncpu] of MAP) {
    const file = `pewter-v${version}-${gos}-${goarch}.tar.gz`;
    const archive = path.join(dist, file);
    if (!fs.existsSync(archive)) continue; // a partial local build is fine
    const got = crypto.createHash("sha256").update(fs.readFileSync(archive)).digest("hex");
    if (sums.get(file) !== got) throw new Error(`checksum mismatch for ${file}`);
    const name = `@omegacompute/pewter-${nos}-${ncpu}`;
    const dir = path.join(out, `pewter-${nos}-${ncpu}`);
    fs.mkdirSync(path.join(dir, "bin"), { recursive: true });
    execFileSync("tar", ["-xzf", archive, "-C", path.join(dir, "bin"), "pewter"]);
    fs.chmodSync(path.join(dir, "bin", "pewter"), 0o755);
    fs.writeFileSync(path.join(dir, "package.json"), JSON.stringify({
      name, version, description: `pewter binary for ${nos}/${ncpu}`, private: true, license: "Apache-2.0",
      os: [nos], cpu: [ncpu], files: ["bin/pewter"], homepage: "https://omegacompute.com",
    }, null, 2) + "\n");
    made.push(name);
  }
  return made;
}

if (require.main === module) {
  const [version, dist, out] = process.argv.slice(2);
  if (!version || !dist || !out) { console.error("usage: build-platform-packages.js <version> <dist> <out>"); process.exit(2); }
  console.log(build(version, dist, out).join("\n"));
}
module.exports = { build };
