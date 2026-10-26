"use strict";
const test = require("node:test");
const assert = require("node:assert");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const { resolveBinary, PLATFORMS } = require("../pewter/bin/pewter.js");

const pkgJson = require("../pewter/package.json");
const launcher = path.join(__dirname, "../pewter/bin/pewter.js");

test("package exposes both bins to the same launcher, no install scripts", () => {
  assert.strictEqual(pkgJson.name, "@omegacompute/pewter");
  assert.deepStrictEqual(Object.keys(pkgJson.bin).sort(), ["pew", "pewter"]);
  assert.strictEqual(pkgJson.bin.pew, pkgJson.bin.pewter);
  for (const k of ["preinstall", "install", "postinstall"]) assert.ok(!(pkgJson.scripts || {})[k], k);
});

test("every supported platform is an optional dependency", () => {
  assert.deepStrictEqual(Object.keys(pkgJson.optionalDependencies).sort(), Object.values(PLATFORMS).sort());
});

test("unsupported platform and missing package give clear errors", () => {
  assert.throws(() => resolveBinary("win32", "x64"), /no build for win32\/x64/);
  assert.throws(() => resolveBinary("linux", "x64", () => { throw new Error("nope"); }), /not installed/);
});

test("launcher runs the platform binary, passes args and exit codes", { skip: process.platform === "win32" }, () => {
  const key = `${process.platform}-${process.arch}`;
  if (!PLATFORMS[key]) return;
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "pewter-npm-"));
  const dir = path.join(root, "node_modules", ...PLATFORMS[key].split("/"), "bin");
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(path.join(dir, "pewter"), '#!/bin/sh\necho "args:$*"\nexit 7\n', { mode: 0o644 }); // not executable on purpose
  fs.mkdirSync(path.join(root, "node_modules", ".bin"), { recursive: true });
  const nm = path.join(root, "node_modules", "@omegacompute", "pewter", "bin");
  fs.mkdirSync(nm, { recursive: true });
  fs.copyFileSync(launcher, path.join(nm, "pewter.js"));
  // call it as `pew` too (symlink, as npm does for the second bin)
  fs.symlinkSync("pewter.js", path.join(nm, "pew"));
  for (const entry of ["pewter.js", "pew"]) {
    const r = spawnSync(process.execPath, [path.join(nm, entry), "orders", "--json"], { encoding: "utf8" });
    assert.strictEqual(r.status, 7);
    assert.strictEqual(r.stdout.trim(), "args:orders --json");
  }
});

test("build-platform-packages verifies checksums and writes os/cpu-scoped packages", () => {
  const { build } = require("../scripts/build-platform-packages.js");
  const crypto = require("node:crypto");
  const { execFileSync } = require("node:child_process");
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "pewter-npm-b-"));
  const dist = path.join(root, "dist"), out = path.join(root, "out"), stage = path.join(root, "stage");
  fs.mkdirSync(dist); fs.mkdirSync(stage);
  fs.writeFileSync(path.join(stage, "pewter"), "bin");
  const f = "pewter-v1.2.3-linux-amd64.tar.gz";
  execFileSync("tar", ["-C", stage, "-czf", path.join(dist, f), "pewter"]);
  const h = crypto.createHash("sha256").update(fs.readFileSync(path.join(dist, f))).digest("hex");
  fs.writeFileSync(path.join(dist, "SHA256SUMS"), `${h}  ${f}\n`);
  assert.deepStrictEqual(build("1.2.3", dist, out), ["@omegacompute/pewter-linux-x64"]);
  const p = JSON.parse(fs.readFileSync(path.join(out, "pewter-linux-x64", "package.json")));
  assert.deepStrictEqual([p.os, p.cpu], [["linux"], ["x64"]]);
  fs.writeFileSync(path.join(dist, "SHA256SUMS"), `${"0".repeat(64)}  ${f}\n`);
  assert.throws(() => build("1.2.3", dist, out), /checksum mismatch/);
});
