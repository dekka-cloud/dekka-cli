"use strict";

const assert = require("node:assert/strict");
const { spawnSync } = require("node:child_process");
const { createHash } = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { test } = require("node:test");
const { ensure, expected, target } = require("../lib/install");

test("each platform has its own archive", () => {
  assert.deepEqual(target("darwin", "arm64"), { archive: "dekka_darwin_arm64.tar.gz", binary: "dekka" });
  assert.deepEqual(target("linux", "x64"), { archive: "dekka_linux_amd64.tar.gz", binary: "dekka" });
  assert.deepEqual(target("win32", "x64"), { archive: "dekka_windows_amd64.zip", binary: "dekka.exe" });
  assert.throws(() => target("freebsd", "x64"), /no dekka release for freebsd on x64/);
  assert.throws(() => target("linux", "ia32"), /no dekka release for linux on ia32/);
});

test("the checksum comes from the release's list", () => {
  const sum = "a".repeat(64);
  assert.equal(expected(`${"b".repeat(64)}  dekka_linux_amd64.tar.gz\n${sum}  dekka_darwin_arm64.tar.gz\n`, "dekka_darwin_arm64.tar.gz"), sum);
  assert.throws(() => expected("", "dekka_darwin_arm64.tar.gz"), /no entry for dekka_darwin_arm64.tar.gz/);
});

/** A release archive holding a script that stands in for the binary. */
function release() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "dekka-release-"));
  fs.writeFileSync(path.join(dir, "dekka"), "#!/bin/sh\necho dekka test\n");
  fs.writeFileSync(path.join(dir, "README.md"), "readme\n");
  const made = spawnSync("tar", ["-czf", path.join(dir, "a.tar.gz"), "-C", dir, "dekka", "README.md"]);
  assert.equal(made.status, 0);
  const bytes = fs.readFileSync(path.join(dir, "a.tar.gz"));
  return { bytes, checksums: `${createHash("sha256").update(bytes).digest("hex")}  dekka_linux_amd64.tar.gz\n` };
}

function serving(bytes, calls) {
  return async (url) => {
    calls.push(url);
    return { ok: true, status: 200, arrayBuffer: async () => bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) };
  };
}

test("the binary is downloaded once, checked and kept", { skip: process.platform === "win32" }, async () => {
  const { bytes, checksums } = release();
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "dekka-cache-"));
  const calls = [];
  const options = { version: "1.2.3", checksums, dir, fetch: serving(bytes, calls), log: () => {}, platform: "linux", arch: "x64" };

  const binary = await ensure(options);
  assert.equal(binary, path.join(dir, "dekka"));
  assert.deepEqual(calls, ["https://github.com/dekka-cloud/dekka-cli/releases/download/v1.2.3/dekka_linux_amd64.tar.gz"]);
  assert.equal(String(spawnSync(binary).stdout), "dekka test\n");
  // Only the binary is kept, and the second run downloads nothing.
  assert.deepEqual(fs.readdirSync(dir), ["dekka"]);
  assert.equal(await ensure(options), binary);
  assert.equal(calls.length, 1);
});

test("a download that does not match the release is refused", async () => {
  const { bytes, checksums } = release();
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "dekka-cache-"));
  const changed = Buffer.concat([bytes, Buffer.from("x")]);
  const options = { version: "1.2.3", checksums, dir, fetch: serving(changed, []), log: () => {}, platform: "linux", arch: "x64" };
  await assert.rejects(ensure(options), /checksum of dekka_linux_amd64.tar.gz does not match/);
  assert.equal(fs.existsSync(path.join(dir, "dekka")), false);
});

test("a release that is missing says so", async () => {
  const { checksums } = release();
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "dekka-cache-"));
  const missing = async () => ({ ok: false, status: 404 });
  await assert.rejects(ensure({ version: "9.9.9", checksums, dir, fetch: missing, log: () => {}, platform: "linux", arch: "x64" }), /HTTP 404/);
});
