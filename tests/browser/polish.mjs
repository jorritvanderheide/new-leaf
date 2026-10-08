// The details: contrast (light and dark), keyboard and touch, page breaks
// and fit hints, thumbnails and the accent warning.

import { api, check, openPage, sleep } from "./lib.mjs";

const W = "Alpine.$data(document.querySelector('[x-data=workspace]'))";

export default async function polish({ base, shots }) {
  const page = await openPage({ shots });
  const ready = async () => {
    await sleep(500);
    await page.until(`!${W}.loading && document.querySelectorAll('[x-ref=pages] [role=button]').length > 0`, "preview", 30000);
  };
  try {
    // Text contrast, everywhere (WCAG AA).
    for (const path of ["/", "/v/full-cv/", "/items/", "/profile/"]) {
      await page.go(base + path);
      await sleep(800);
      const low = await page.lowContrast();
      check(low.length === 0, `${path}: all text has enough contrast ${JSON.stringify(low)}`);
    }

    // Dark mode follows the system unless chosen otherwise, and keeps the contrast.
    await page.send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: "dark" }] });
    for (const path of ["/", "/v/full-cv/", "/items/", "/profile/"]) {
      await page.go(base + path);
      await sleep(800);
      check(await page.js("document.documentElement.classList.contains('dark')"), `${path}: dark, as the system is`);
      const low = await page.lowContrast();
      check(low.length === 0, `${path}: dark text has enough contrast ${JSON.stringify(low)}`);
    }
    await page.js("document.querySelector('body > aside [aria-haspopup=menu]').click()");
    await sleep(300);
    await page.click("[aria-label=Appearance] button", "light");
    check(!(await page.js("document.documentElement.classList.contains('dark')")), "Light overrides the system");
    await page.go(base + "/");
    check(!(await page.js("document.documentElement.classList.contains('dark')")), "and is remembered");
    await page.js("Alpine.store('cv').setMode('system')");
    check(await page.js("document.documentElement.classList.contains('dark')"), "System follows the system again");
    await page.send("Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: "light" }] });
    await sleep(100);
    check(!(await page.js("document.documentElement.classList.contains('dark')")), "including when the system changes");

    // Page breaks in the outline, and how far from the target the pages are.
    await page.go(base + "/v/full-cv/");
    await ready();
    const pages = await page.js(`${W}.pages`);
    const dividers = await page.js("[...document.querySelectorAll('aside[aria-label=Outline] [aria-hidden=true]')].map((d) => d.textContent.trim()).filter((t) => t.startsWith('Page'))");
    check(dividers.length === pages - 1 && dividers[0] === "Page 2", `the outline shows where pages start: ${dividers} (${pages} pages)`);
    const hint = await page.js(`${W}.fitHint()`);
    check(/^~\d+ lines over|^Room for ~\d+ more lines$/.test(hint), "fit hint: " + hint);

    // Sections move with Alt+arrow keys; the handle keeps the focus.
    const before = await page.js(`${W}.shownSections()`);
    await page.js(`document.querySelector('[data-handle="${before[0]}"]').focus()`);
    await page.send("Input.dispatchKeyEvent", { type: "keyDown", key: "ArrowDown", code: "ArrowDown", modifiers: 1, windowsVirtualKeyCode: 40 });
    await sleep(300);
    const after = await page.js(`${W}.shownSections()`);
    check(after[1] === before[0] && after[0] === before[1], `Alt+↓ moved ${before[0]} down`);
    check((await page.js("document.activeElement.dataset.handle")) === before[0], "and the handle kept the focus");
    await page.js(`${W}.undo()`);
    await sleep(300);

    // A hard-to-read accent gets a warning and a darker shade.
    await page.click("button", "Look");
    await page.js(`${W}.theme.accent = '#f5c518'`);
    await sleep(200);
    check(await page.js("!!document.querySelector('[role=dialog][aria-label=Look] [role=status]').offsetParent"), "a pale accent is flagged");
    await page.click("[role=dialog][aria-label=Look] button", "Use a darker shade");
    await sleep(200);
    check((await page.js(`${W}.accentContrast()`)) >= 4.5, `the darker shade is readable: ${await page.js(`${W}.theme.accent`)}`);
    check(!(await page.js("document.querySelector('[role=dialog][aria-label=Look] [role=status]').offsetParent")), "and the warning is gone");
    await page.js(`${W}.theme.accent = ''`);
    await page.js(`${W}.lookOpen = false`);

    // Thumbnails on the overview.
    await page.go(base + "/");
    await page.until("[...document.querySelectorAll('article img')].length === Alpine.store('cv').state.versions.length && [...document.querySelectorAll('article img')].every((i) => i.complete && i.naturalWidth > 0)", "thumbnails", 30000);
    check(true, "every version has a thumbnail");
    check(
      (await api(base, "GET", "/api/state")).versions.every((v) => v.pages > 0),
      "and a page count",
    );

    // Phones: the name has a row of its own, checkboxes a finger-sized target.
    await page.viewport(390, 844);
    await page.go(base + "/v/full-cv/");
    await sleep(800);
    check((await page.js("document.querySelector('[aria-label=\"Version name\"]').getBoundingClientRect().width")) > 250, "the version name has room on a phone");
    const target = await page.js("(() => { const r = document.querySelector('aside[aria-label=Outline] label').getBoundingClientRect(); return Math.min(r.width, r.height); })()");
    check(target >= 44, `checkbox tap target ${target}px`);
    check((await page.js("document.querySelectorAll('header [aria-haspopup=menu]').length")) === 1, "the CV menu is in the top bar");

    check(page.errors.length === 0, "no uncaught JS errors " + JSON.stringify(page.errors));
  } finally {
    await page.close();
  }
}
