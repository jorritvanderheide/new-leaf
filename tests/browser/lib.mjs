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
    headers: { "X-New-Leaf": "1", "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`${method} ${path}: ${res.status} ${await res.text()}`);
  return res.status === 204 ? null : res.json();
}

// openPage starts a fresh browser (empty profile, so no cookies) with one page.
export async function openPage({ width = 1440, height = 900, shots } = {}) {
  const profile = mkdtempSync(join(tmpdir(), "new-leaf-chromium-"));
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

    // lowContrast lists visible text below the WCAG AA contrast (4.5:1, or
    // 3:1 for large text) against what is behind it. Disabled controls and
    // placeholders are exempt, as in WCAG.
    lowContrast() {
      return page.js(`(() => {
        const ctx = document.createElement('canvas').getContext('2d', { willReadFrequently: true });
        const rgba = (css) => { ctx.clearRect(0, 0, 1, 1); ctx.fillStyle = '#000'; ctx.fillStyle = css; ctx.fillRect(0, 0, 1, 1); const d = ctx.getImageData(0, 0, 1, 1).data; return [d[0], d[1], d[2], d[3] / 255]; };
        const lum = ([r, g, b]) => [r, g, b].map((c) => c / 255).map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4)).reduce((s, c, i) => s + c * [0.2126, 0.7152, 0.0722][i], 0);
        const over = (top, below) => top.slice(0, 3).map((c, i) => c * top[3] + below[i] * (1 - top[3]));
        const background = (el) => {
          const layers = [];
          for (let e = el; e; e = e.parentElement) {
            const bg = rgba(getComputedStyle(e).backgroundColor);
            if (bg[3] > 0) layers.push(bg);
            if (bg[3] >= 1) break;
          }
          return layers.reduceRight((below, top) => over(top, below), [255, 255, 255]);
        };
        const out = [];
        const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
        for (let n = walker.nextNode(); n; n = walker.nextNode()) {
          const el = n.parentElement;
          if (!n.textContent.trim() || !el.checkVisibility({ opacityProperty: true, visibilityProperty: true })) continue;
          if (el.closest('[disabled], [aria-disabled=true], [aria-hidden=true], option, script, style')) continue;
          const style = getComputedStyle(el);
          const bg = background(el);
          const fg = over(rgba(style.color), bg);
          const [a, b] = [lum(fg), lum(bg)].sort((x, y) => y - x);
          const ratio = (a + 0.05) / (b + 0.05);
          const size = parseFloat(style.fontSize), bold = +style.fontWeight >= 700;
          const need = size >= 24 || (size >= 18.66 && bold) ? 3 : 4.5;
          if (ratio < need - 0.05) out.push(n.textContent.trim().slice(0, 30) + ' ' + ratio.toFixed(2));
        }
        return [...new Set(out)];
      })()`);
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
      rmSync(profile, { recursive: true, force: true, maxRetries: 5 }); // its helpers may still be writing
    },
  };
  await page.viewport(width, height);
  return page;
}
