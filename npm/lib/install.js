"use strict";

// Fetches the dekka binary for this machine from the CLI's GitHub release and
// keeps it for the next run. The archive is checked against checksums.txt,
// which is the release's own list and is published inside this package, so a
// download that was changed on the way is refused.
const { spawnSync } = require("node:child_process");
const { createHash } = require("node:crypto");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const REPO = "dekka-cloud/dekka-cli";
const SYSTEMS = { darwin: "darwin", linux: "linux", win32: "windows" };
const ARCHITECTURES = { x64: "amd64", arm64: "arm64" };

/** The release archive and the binary inside it for a platform. */
function target(platform = process.platform, arch = process.arch) {
  const system = SYSTEMS[platform];
  const architecture = ARCHITECTURES[arch];
  if (!system || !architecture) throw new Error(`there is no dekka release for ${platform} on ${arch}`);
  const windows = system === "windows";
  return { archive: `dekka_${system}_${architecture}.${windows ? "zip" : "tar.gz"}`, binary: windows ? "dekka.exe" : "dekka" };
}

/** The SHA-256 that checksums.txt records for an archive. */
function expected(checksums, archive) {
  for (const line of checksums.split("\n")) {
    const [sum, name] = line.trim().split(/\s+/);
    if (name === archive && /^[0-9a-f]{64}$/.test(sum)) return sum;
  }
  throw new Error(`checksums.txt has no entry for ${archive}`);
}

function cacheDir(version, env = process.env) {
  if (env.DEKKA_CACHE_DIR) return path.join(env.DEKKA_CACHE_DIR, version);
  if (process.platform === "win32" && env.LOCALAPPDATA) return path.join(env.LOCALAPPDATA, "dekka", version);
  return path.join(env.XDG_CACHE_HOME || path.join(os.homedir(), ".cache"), "dekka", version);
}

function packaged() {
  const root = path.join(__dirname, "..");
  const { version } = JSON.parse(fs.readFileSync(path.join(root, "package.json"), "utf8"));
  const file = path.join(root, "checksums.txt");
  if (!fs.existsSync(file)) throw new Error("this copy of the package carries no release, so install dekka from npm or build it with Go");
  return { version, checksums: fs.readFileSync(file, "utf8") };
}

/**
 * Returns the path of the binary for this machine, downloading it on first use.
 * Every argument has a default, and tests replace them.
 */
async function ensure({ version, checksums, dir, fetch: get = globalThis.fetch, log = (line) => console.error(line), platform, arch } = {}) {
  if (version === undefined || checksums === undefined) ({ version, checksums } = packaged());
  const { archive, binary } = target(platform, arch);
  const home = dir ?? cacheDir(version);
  const installed = path.join(home, binary);
  if (fs.existsSync(installed)) return installed;

  const want = expected(checksums, archive);
  const url = `https://github.com/${REPO}/releases/download/v${version}/${archive}`;
  log(`Downloading dekka ${version}.`);
  const response = await get(url, { redirect: "follow" });
  if (!response.ok) throw new Error(`could not download ${url} (HTTP ${response.status})`);
  const bytes = Buffer.from(await response.arrayBuffer());
  const got = createHash("sha256").update(bytes).digest("hex");
  if (got !== want) throw new Error(`the checksum of ${archive} does not match the release's, so nothing was installed`);

  // Unpack beside the final place and move the binary in, so a run that is
  // interrupted never leaves half a binary where the next run looks.
  fs.mkdirSync(home, { recursive: true });
  const work = fs.mkdtempSync(path.join(home, ".unpack-"));
  try {
    const file = path.join(work, archive);
    fs.writeFileSync(file, bytes);
    const untar = spawnSync("tar", ["-xf", file, "-C", work, binary], { stdio: ["ignore", "ignore", "pipe"] });
    if (untar.error || untar.status !== 0) {
      throw new Error(`could not unpack ${archive}: ${untar.error ? untar.error.message : String(untar.stderr).trim()}`);
    }
    fs.chmodSync(path.join(work, binary), 0o755);
    fs.renameSync(path.join(work, binary), installed);
  } finally {
    fs.rmSync(work, { recursive: true, force: true });
  }
  return installed;
}

module.exports = { ensure, expected, target, cacheDir };
