// Versions: make one, edit an item from its preview, hide items, reorder
// sections, and duplicate, delete and undo on the overview.

import { api, check, openPage, sleep } from "./lib.mjs";

const W = "Alpine.$data(document.querySelector('[x-data=workspace]'))";
const marks = "document.querySelectorAll('[x-ref=pages] [data-mark]')";

export default async function versions({ base, shots }) {
  const page = await openPage({ shots });
  const state = () => api(base, "GET", "/api/state");
  const version = async (id) => (await state()).versions.find((v) => v.id === id);
  const rendered = async () => {
    await sleep(500);
    await page.until(`!${W}.loading && ${marks}.length > 0`, "preview", 30000);
  };
  try {
    // New version, from all items.
    await page.go(base + "/");
    await page.click("button", "New version");
    await page.until("document.activeElement?.id === 'version-name'", "name field", 3000);
    await page.type("#version-name", "UvA PhD");
    await page.click("form button", "Create");
    await page.until("location.pathname === '/v/uva-phd/'", "the new version opens", 8000);
    await page.until("!!window.Alpine && !!Alpine.store('cv').state", "editor state");
    await rendered();
    const all = (await version("uva-phd")).entries.length;
    check(all === (await state()).items.length, `the version starts with all ${all} items`);
    check((await page.js(`${marks}.length`)) === all, "every item on the preview is clickable");

    // Click an item in the preview: it opens in the editor; edits show.
    const first = await page.js(`${marks}[0].getAttribute('aria-label')`);
    await page.js(`${marks}[0].click()`);
    await page.until("!!document.querySelector('#title-en')", "item editor", 5000);
    const title = await page.js("document.querySelector('#title-en').value");
    check(first === title, `clicking "${title}" in the preview opened it`);
    check(await page.js(`${marks}[0].className.includes('ring-accent')`), "the open item is marked in the preview");
    const round = await page.js(`${W}.pageList[0].key`);
    await page.type("#title-en", title + " (live)");
    await page.until(`${W}.pageList[0]?.key !== ${JSON.stringify(round)} && !${W}.loading`, "preview redrawn after the edit", 30000);
    check(true, "the preview redraws as the item is edited");
    await page.shot("workspace-editing");
    await page.type("#title-en", title);
    await page.key("Escape");
    await page.until("!document.querySelector('#title-en')", "editor closed", 5000);

    // Hide an item from the preview.
    const hidden = await page.js(`${W}.pageList[0].marks[1].id`);
    await page.js(`${marks}[1].dispatchEvent(new MouseEvent('mouseenter'))`);
    await page.js(`${marks}[1].querySelector('button[aria-label^=Hide]').click()`);
    await page.until(`!${W}.selected.includes(${JSON.stringify(hidden)})`, "hidden", 3000);
    await rendered();
    await sleep(1000);
    check(!(await version("uva-phd")).entries.includes(hidden), "Hide takes the item off this version");
    check((await version("full-cv")).entries.includes(hidden), "and leaves other versions alone");

    // Undo and redo: the hide comes back, and goes again with Ctrl+Shift+Z.
    await page.js("document.querySelector('button[aria-label=Undo]').click()");
    await page.until(`${W}.selected.includes(${JSON.stringify(hidden)})`, "undone", 3000);
    await sleep(1200);
    check((await version("uva-phd")).entries.includes(hidden), "Undo brings a hidden item back, and saves that");
    await page.key("z", 2 | 8); // Ctrl+Shift
    await page.until(`!${W}.selected.includes(${JSON.stringify(hidden)})`, "redone", 3000);
    check(true, "Ctrl+Shift+Z redoes it");

    // Reorder sections by dragging.
    const before = await page.js(`${W}.shownSections()`);
    await page.js(`(() => {
      const cards = [...document.querySelectorAll('aside[aria-label=Outline] [data-handle]')].map((h) => h.closest('.card'));
      const last = cards[cards.length - 1], top = cards[0];
      const dt = new DataTransfer();
      last.querySelector('[data-handle]').dispatchEvent(new PointerEvent('pointerdown', { bubbles: true, pointerType: 'mouse' }));
      return Alpine.nextTick(() => {
        last.dispatchEvent(new DragEvent('dragstart', { bubbles: true, dataTransfer: dt }));
        top.dispatchEvent(new DragEvent('dragover', { bubbles: true, cancelable: true, dataTransfer: dt }));
        last.dispatchEvent(new DragEvent('dragend', { bubbles: true, dataTransfer: dt }));
      });
    })()`);
    await sleep(1200);
    const shown = await page.js(`${W}.shownSections()`);
    check(shown[0] === before[before.length - 1], `dragged ${shown[0]} to the top`);
    check((await version("uva-phd")).order.find((s) => shown.includes(s)) === shown[0], "the new order is saved");
    await rendered();
    check((await page.js("document.querySelector('[x-ref=pages] [data-mark]').getAttribute('aria-label')")) !== first, "the preview follows the new order");
    await page.js("document.querySelector('button[aria-label=Undo]').click()");
    await page.until(`${W}.shownSections()[0] === ${JSON.stringify(before[0])}`, "drag undone", 3000);
    check(true, "Undo takes a whole drag back in one step");

    // The same with a finger, which gets no drag and drop from the browser.
    await page.js(`(() => {
      const handles = [...document.querySelectorAll('aside[aria-label=Outline] [data-handle]')];
      const top = handles[0].closest('.card');
      top.scrollIntoView();
      const r = top.getBoundingClientRect();
      const at = { bubbles: true, pointerType: 'touch', clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 };
      handles[handles.length - 1].dispatchEvent(new PointerEvent('pointerdown', at));
      dispatchEvent(new PointerEvent('pointermove', at));
      dispatchEvent(new PointerEvent('pointerup', at));
    })()`);
    await page.until(`${W}.shownSections()[0] === ${JSON.stringify(before[before.length - 1])}`, "touch drag", 3000);
    check(true, "a touch drag on the handle moves a section too");
    await page.js("document.querySelector('button[aria-label=Undo]').click()");
    await page.until(`${W}.shownSections()[0] === ${JSON.stringify(before[0])}`, "touch drag undone", 3000);

    // The same with the keyboard, as a screen reader hears it.
    await page.js("document.querySelector('aside[aria-label=Outline] [data-handle]').focus()");
    await page.key("ArrowDown", 1); // Alt
    await page.until(`${W}.shownSections()[1] === ${JSON.stringify(before[0])}`, "moved down", 3000);
    check((await page.js("document.activeElement.dataset.handle")) === before[0], "Alt+↓ moves a section, and its handle keeps focus");
    check(/moved to place 2 of/.test(await page.js("document.querySelector('aside[aria-label=Outline] [aria-live]').textContent")), "and says where it went");
    await page.js("document.querySelector('button[aria-label=Undo]').click()");
    await page.until(`${W}.shownSections()[0] === ${JSON.stringify(before[0])}`, "move undone", 3000);

    // In the preview, each item is a group with its own buttons, which show
    // when they have focus.
    check((await page.js("document.querySelectorAll('[role=button] button, [role=button] [role=button], button button').length")) === 0, "no button inside another");
    await page.js(`${marks}[0].querySelector('button[aria-label^=Hide]').focus()`);
    check((await page.js(`getComputedStyle(${marks}[0].querySelector('button[aria-label^=Hide]').parentElement).opacity`)) === "1", "Hide shows when it has focus");
    check((await page.js(`${marks}[0].getAttribute('role') === 'group' && !!${marks}[0].getAttribute('aria-label')`)), "items in the preview are named groups");

    // Look: colour and font, saved with the version; a default for new ones.
    await page.click("button", "Look");
    await page.until("!!document.querySelector('[role=dialog][aria-label=Look]').offsetParent", "look menu", 3000);
    await page.js("document.querySelector('button[aria-label=Crimson]').click()");
    await page.click("[aria-label=Font] button", "Serif");
    await sleep(1500);
    const look = (await version("uva-phd")).theme;
    check(look.accent === "#b91c1c" && look.font === "serif", "the look is saved: " + JSON.stringify(look));
    await rendered();
    check(await page.js(`${W}.pages > 0`), "the preview renders in the new look");

    // The first page starts a little under the settings bar. An inner shadow
    // shows where the preview has more to scroll below; above, the bar's own
    // shadow does, as the pages scroll under it.
    const pane = "document.querySelector('[x-ref=pages]').parentElement.parentElement";
    const gap = await page.js("document.querySelector('[x-ref=pages] canvas').getBoundingClientRect().top - document.querySelector('[data-settings]').getBoundingClientRect().bottom");
    check(Math.abs(gap - 24) < 1, `the first page starts 24px under the settings bar (${gap}px)`);
    check(await page.js(`${pane}.parentElement.hasAttribute('data-below')`), "a shadow below the preview");
    await page.js(`${pane}.scrollTop = 200`);
    await sleep(200);
    check(await page.js(`getComputedStyle(${pane}.parentElement, '::before').display === 'none'`), "none above once scrolled: the settings bar has its own");
    const above = await page.js(`(() => {
      const r = document.querySelector('[data-settings]').getBoundingClientRect();
      return !!document.elementFromPoint(r.left + r.width / 2, r.top - 4).closest('[x-ref=pages]');
    })()`);
    check(!above, "the scrolled pages are cut off at the settings bar");
    await page.js(`${pane}.scrollTop = 0`);
    const spacing = await page.js(`${W}.spacing`);
    await page.js(`${W}.spacing = 0.85`);
    await page.click("[role=dialog][aria-label=Look] button", "Use look and section order for new versions");
    await page.until("Alpine.store('cv').state.profile.theme?.font === 'serif' && Alpine.store('cv').state.profile.spacing === 0.85", "default look", 5000);
    const fresh = (await api(base, "POST", "/api/versions", { name: "Look check" })).versions.find((v) => v.id === "look-check");
    check(fresh.theme.accent === "#b91c1c" && fresh.theme.font === "serif" && fresh.spacing === 0.85, "new versions start with the default look and spacing");
    await api(base, "DELETE", "/api/versions/look-check");
    const profile = (await state()).profile;
    await api(base, "PUT", "/api/profile", { ...profile, theme: {}, spacing: 0 });
    await page.js(`${W}.spacing = ${spacing}`);

    // The overview: duplicate, delete, undo.
    await page.go(base + "/");
    check((await page.js("document.querySelector('article h2').textContent")) === "UvA PhD", "the latest edited version comes first");
    await page.js("[...document.querySelectorAll('article')].find((a) => a.textContent.includes('UvA PhD')).querySelector('button').click()");
    await page.until("Alpine.store('cv').state.versions.some((v) => v.name === 'Copy of UvA PhD')", "duplicate", 5000);
    check(true, "duplicated");
    await page.js("[...document.querySelectorAll('article')].find((a) => a.textContent.includes('Copy of UvA PhD')).querySelectorAll('button')[1].click()");
    await page.until("document.body.textContent.includes('Undo')", "undo toast", 5000);
    check(!(await state()).versions.some((v) => v.name === "Copy of UvA PhD"), "deleted without a question");
    await page.click("button", "Undo");
    await page.until("Alpine.store('cv').state.versions.some((v) => v.name === 'Copy of UvA PhD')", "restored", 5000);
    check(true, "Undo brought it back");
    await page.shot("versions");

    for (const v of (await state()).versions.filter((v) => v.id !== "full-cv")) await api(base, "DELETE", `/api/versions/${v.id}`);
    check(page.dialogs.length === 0, "no dialogs " + JSON.stringify(page.dialogs));
    check(page.errors.length === 0, "no uncaught JS errors " + JSON.stringify(page.errors));
  } finally {
    await page.close();
  }
}
