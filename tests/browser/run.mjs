// Runs the browser tests against a fresh server holding a made-up CV, then
// against local mode. Needs new-leaf (or $NEW_LEAF), chromium and typst:
//
//   nix run .#browser-tests [-- suite...]
//
// Screenshots go to $SHOTS if set.

import { spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { seed } from "./fixture.mjs";
import cvs from "./cvs.mjs";
import editor from "./editor.mjs";
import local from "./local.mjs";
import preview from "./preview.mjs";
import share from "./share.mjs";
import versions from "./versions.mjs";
import polish from "./polish.mjs";
import widths from "./widths.mjs";

const server = { editor, preview, widths, share, versions, polish, cvs };
const only = process.argv.slice(2);
const bin = process.env.NEW_LEAF || "new-leaf";
const shots = process.env.SHOTS;

const freePort = () =>
  new Promise((resolve) => {
    const s = createServer().listen(0, "127.0.0.1", () => {
      const { port } = s.address();
      s.close(() => resolve(port));
    });
  });

// start runs new-leaf and resolves with its editor URL, read from its output.
function start(args, pattern) {
  const proc = spawn(bin, args, { stdio: ["ignore", "pipe", "pipe"] });
  let out = "";
  return new Promise((resolve, reject) => {
    const read = (chunk) => {
      out += chunk;
      const m = pattern.exec(out);
      if (m) resolve({ proc, url: m[1].replace(/\/$/, "") });
    };
    proc.stdout.on("data", read);
    proc.stderr.on("data", read);
    proc.on("exit", (code) => reject(new Error(`${bin} exited (${code}):\n${out}`)));
  });
}

async function suite(name, fn, env) {
  if (only.length && !only.includes(name)) return true;
  console.log(name);
  try {
    await fn(env);
    return true;
  } catch (e) {
    console.error(`  FAIL ${e.message}`);
    return false;
  }
}

const data = mkdtempSync(join(tmpdir(), "new-leaf-browser-"));
const procs = [];
let ok = true;
try {
  const pub = await freePort();
  const srv = await start(
    ["serve", "-dev-user", "alice", "-listen", "127.0.0.1:0", "-data", join(data, "server"), "-public", join(data, "public"),
      "-public-url", `http://127.0.0.1:${pub}`, "-serve-public", `127.0.0.1:${pub}`],
    /editor listening on (\S+)/,
  );
  procs.push(srv.proc);
  const base = srv.url.replace(/^(?!http)/, "http://");
  await seed(base);
  for (const [name, fn] of Object.entries(server)) ok = (await suite(name, fn, { base, shots })) && ok;

  if (!only.length || only.includes("local")) {
    const loc = await start(["-no-browser", "-listen", "127.0.0.1:0", "-data", join(data, "local")], /running at (\S+)/);
    procs.push(loc.proc);
    ok = (await suite("local", local, { base: loc.url, shots })) && ok;
  }
} finally {
  for (const p of procs) p.kill();
  rmSync(data, { recursive: true, force: true });
}
console.log(ok ? "all passed" : "FAILED");
// Exit even if a browser or server is still winding down.
process.exit(ok ? 0 : 1);
