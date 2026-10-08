// A small driver for headless Chromium over the DevTools protocol, so the
// browser tests need no npm packages: just node and chromium.

import { spawn } from "node:child_process";
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

export const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

export const check = (cond, msg) => {
  if (!cond) throw new Error(msg);
  console.log("  ok  " + msg);
};

// api calls the editor like the UI does.
export async function api(base, method, path, body) {
  const res = await fetch(base + path, {
    method,
    headers: { "X-CV-App": "1", "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`${method} ${path}: ${res.status} ${await res.text()}`);
  return res.status === 204 ? null : res.json();
}

// openPage starts a fresh browser (empty profile, so no cookies) with one page.
export async function openPage({ width = 1440, height = 900, shots } = {}) {
  const profile = mkdtempSync(join(tmpdir(), "cv-app-chromium-"));
  const proc = spawn(
    process.env.CHROMIUM || "chromium",
    ["--headless", "--no-sandbox", "--disable-gpu", "--remote-debugging-port=0", `--user-data-dir=${profile}`, "about:blank"],
    { stdio: "ignore" },
  );
  // Chromium writes the port it picked into the profile.
  const portFile = join(profile, "DevToolsActivePort");
  for (let i = 0; i < 100 && !existsSync(portFile); i++) await sleep(100);
  const port = readFileSync(portFile, "utf8").split("\n")[0];
  const targets = await (await fetch(`http://127.0.0.1:${port}/json`)).json();
  const ws = new WebSocket(targets.find((t) => t.type === "page").webSocketDebuggerUrl);
  await new Promise((r) => (ws.onopen = r));

  let id = 0;
  const pending = {};
  const errors = []; // uncaught exceptions in the page
  const dialogs = []; // alert/confirm/prompt, all accepted
  const send = (method, params = {}) =>
    new Promise((r) => {
      pending[++id] = r;
      ws.send(JSON.stringify({ id, method, params }));
    });
  ws.onmessage = (m) => {
    const msg = JSON.parse(m.data);
    if (msg.id && pending[msg.id]) {
      pending[msg.id](msg);
      delete pending[msg.id];
    }
    if (msg.method === "Runtime.exceptionThrown") {
      const d = msg.params.exceptionDetails;
      errors.push(d.exception?.description || `${d.text} ${JSON.stringify(d.exception?.preview ?? d.exception?.value ?? "")}`);
    }
    if (msg.method === "Page.javascriptDialogOpening") {
      dialogs.push(msg.params.message);
      send("Page.handleJavaScriptDialog", { accept: true });
    }
  };
  await send("Runtime.enable");
  await send("Page.enable");

  const page = {
    errors,
    dialogs,
    send,

    async viewport(w, h) {
      await send("Emulation.setDeviceMetricsOverride", { width: w, height: h, deviceScaleFactor: 1, mobile: w < 600 });
    },

    // js evaluates an expression in the page and returns its (JSON) value.
    async js(expr) {
      const res = await send("Runtime.evaluate", { expression: expr, awaitPromise: true, returnByValue: true });
      if (res.result.exceptionDetails) throw new Error(`${expr}: ${res.result.exceptionDetails.exception?.description}`);
      return res.result.result.value;
    },

    async until(expr, what, ms = 20000) {
      const end = Date.now() + ms;
      while (Date.now() < end) {
        if (await page.js(expr)) return;
        await sleep(150);
      }
      throw new Error(`timeout waiting for ${what}`);
    },

    // go opens an editor page and waits for its state.
    async go(url) {
      await send("Page.navigate", { url });
      await sleep(300);
      await page.until("!!window.Alpine && !!Alpine.store('cv').state", "editor state");
      await sleep(200);
    },

    // type sets an input's value the way typing does.
    type(sel, value) {
      return page.js(`(() => {
        const el = document.querySelector(${JSON.stringify(sel)});
        el.value = ${JSON.stringify(value)};
        el.dispatchEvent(new Event('input', { bubbles: true }));
        el.dispatchEvent(new Event('change', { bubbles: true }));
      })()`);
    },

    // click clicks the visible element matching sel whose text is text.
    click(sel, text) {
      return page.js(`(() => {
        const el = [...document.querySelectorAll(${JSON.stringify(sel)})]
          .find((e) => e.offsetParent && e.textContent.trim() === ${JSON.stringify(text)});
        if (!el) throw new Error('no ' + ${JSON.stringify(`${sel} "${text}"`)});
        el.click();
      })()`);
    },

    key(k, modifiers = 0) {
      const code = k.length === 1 ? "Key" + k.toUpperCase() : k;
      const vk = k.length === 1 ? k.toUpperCase().charCodeAt(0) : { Escape: 27, Enter: 13 }[k];
      return send("Input.dispatchKeyEvent", { type: "keyDown", key: k, code, modifiers, windowsVirtualKeyCode: vk });
    },

    async shot(name, clip) {
      if (!shots) return;
      const res = await send("Page.captureScreenshot", { format: "png", ...(clip && { clip: { ...clip, scale: 1 } }) });
      writeFileSync(join(shots, name + ".png"), Buffer.from(res.result.data, "base64"));
    },

    async close() {
      ws.close();
      const exited = new Promise((r) => proc.once("exit", r));
      proc.kill();
      await exited;
      rmSync(profile, { recursive: true, force: true });
    },
  };
  await page.viewport(width, height);
  return page;
}
