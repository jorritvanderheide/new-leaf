// The details: contrast (light and dark), keyboard and touch, page breaks
// and fit hints, thumbnails and the accent warning.

import { api, check, openPage, sleep } from "./lib.mjs";

const W = "Alpine.$data(document.querySelector('[x-data=workspace]'))";

export default async function polish({ base, shots }) {
  const page = await openPage({ shots });
  const ready = async () => {
    await sleep(500);
    await page.until(`!${W}.loading && document.querySelectorAll('[x-ref=pages] [data-mark]').length > 0`, "preview", 30000);
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
    const accent = await page.js(`${W}.theme.accent`);
    await page.click("button", "Look");
    await page.js(`${W}.theme.accent = '#f5c518'`);
    await sleep(200);
    check(await page.js("!!document.querySelector('[role=dialog][aria-label=Look] [role=status]').offsetParent"), "a pale accent is flagged");
    await page.click("[role=dialog][aria-label=Look] button", "Use a darker shade");
    await sleep(200);
    check((await page.js(`${W}.accentContrast()`)) >= 4.5, `the darker shade is readable: ${await page.js(`${W}.theme.accent`)}`);
    check(!(await page.js("document.querySelector('[role=dialog][aria-label=Look] [role=status]').offsetParent")), "and the warning is gone");
    await page.js(`${W}.theme.accent = '${accent}'`);
    await page.js(`${W}.lookOpen = false`);

    // Thumbnails on the overview.
    await page.go(base + "/");
    await page.until("[...document.querySelectorAll('article img')].length === Alpine.store('cv').state.versions.length && [...document.querySelectorAll('article img')].every((i) => i.complete && i.naturalWidth > 0)", "thumbnails", 30000);
    check(true, "every version has a thumbnail");
    check(
      (await api(base, "GET", "/api/state")).versions.every((v) => v.pages > 0),
      "and a page count",
    );

    // Phones: the version's header stands in for the app's top bar and stays
    // at the top, the preview starts near the top, and Items and Preview
    // switch at the bottom; checkboxes have a finger-sized target.
    await page.viewport(390, 844);
    await page.go(base + "/v/full-cv/");
    await sleep(800);
    check(!(await page.js("document.querySelector('body > header').offsetParent")), "no app top bar in a version");
    check((await page.js("document.querySelector('[aria-label=\"Version name\"]').getBoundingClientRect().width")) > 150, "the version name has room on a phone");
    const target = await page.js("(() => { const r = document.querySelector('aside[aria-label=Outline] label').getBoundingClientRect(); return Math.min(r.width, r.height); })()");
    check(target >= 44, `checkbox tap target ${target}px`);
    check((await page.js("document.querySelector('[aria-label=View]').getBoundingClientRect().bottom")) > 844 - 60, "Items and Preview switch at the bottom");
    await page.js("[...document.querySelectorAll('[role=tab]')].find((b) => b.textContent.trim().startsWith('Preview')).click()");
    await page.until("document.querySelectorAll('[x-ref=pages] canvas').length > 0", "phone preview", 30000);
    await sleep(800);
    const top = await page.js("document.querySelector('[x-ref=pages] canvas').getBoundingClientRect().top");
    check(top < 844 / 4, `the preview starts near the top (${Math.round(top)}px)`);
    await page.js("scrollTo(0, 800)");
    await sleep(300);
    const header = await page.js("document.querySelector('[aria-label=\"Version name\"]').getBoundingClientRect().top");
    check(header >= 0 && header < 20, `the header stays at the top while scrolling (${Math.round(header)}px)`);

    // Pinching the preview zooms its pages, around the fingers, and draws
    // them sharp again after.
    const pinch = (from, to) =>
      page.js(`(() => {
        const box = document.querySelector('[x-ref=pages]').parentElement;
        const r = box.getBoundingClientRect();
        const x = r.left + r.width / 2, y = 844 / 2;
        const at = (d) => [1, -1].map((s, i) => new Touch({ identifier: i, target: box, clientX: x + (s * d) / 2, clientY: y }));
        const fire = (type, touches) => box.dispatchEvent(new TouchEvent(type, { bubbles: true, cancelable: true, touches, targetTouches: touches, changedTouches: touches }));
        fire('touchstart', at(${from}));
        fire('touchmove', at(${to}));
        fire('touchend', []);
      })()`);
    const fit = await page.js("document.querySelector('[x-ref=pages]').getBoundingClientRect().width");
    const sharp = await page.js("document.querySelector('[x-ref=pages] canvas').width");
    await pinch(100, 200);
    const zoomed = await page.js("document.querySelector('[x-ref=pages]').getBoundingClientRect().width");
    check(Math.abs(zoomed - 2 * fit) < 2, `pinching zooms the pages (${Math.round(fit)} to ${Math.round(zoomed)}px)`);
    check((await page.js("document.querySelector('[x-ref=pages]').parentElement.scrollLeft")) > fit / 3, "around the fingers");
    check((await page.js("visualViewport.scale")) === 1, "and not the whole page");
    const box = await page.js("(() => { const r = document.querySelector('[x-ref=pages]').parentElement.getBoundingClientRect(); return [r.left, r.right]; })()");
    check(box[0] === 0 && box[1] === 390, `the zoomed pages reach the edges of the screen (${box})`);
    const over = await page.js(`(() => {
      const r = document.querySelector('[data-settings]').getBoundingClientRect();
      const under = (y) => document.elementFromPoint(195, y).closest('[x-ref=pages]');
      return r.top > document.querySelector('[aria-label="Version name"]').getBoundingClientRect().bottom && !!under(r.top - 4) && !!under(r.bottom + 4);
    })()`);
    check(over, "the settings float under the header, over the scrolled pages");
    await page.until(`document.querySelector('[x-ref=pages] canvas').width > ${sharp * 1.9}`, "zoomed redraw", 10000);
    check(true, "the zoomed pages are drawn sharp");
    await pinch(200, 100);
    check(Math.abs((await page.js("document.querySelector('[x-ref=pages]').getBoundingClientRect().width")) - fit) < 2, "pinching back fits them again");
    await page.js("document.querySelector('[aria-label=More]').click()");
    await sleep(300);
    check((await page.js("[...document.querySelectorAll('[role=menu] [role=menuitem]')].filter((m) => m.offsetParent).map((m) => m.textContent.trim()).join()")) === "Download PDF,Undo,Redo", "Download, Undo and Redo are in the ⋯ menu");
    await page.go(base + "/");
    await sleep(500);
    check((await page.js("document.querySelectorAll('header [aria-haspopup=menu]').length")) === 1, "elsewhere the CV menu is in the top bar");

    check(page.errors.length === 0, "no uncaught JS errors " + JSON.stringify(page.errors));
  } finally {
    await page.close();
  }
}
