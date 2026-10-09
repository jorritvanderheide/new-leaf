// Editing: autosave, validation, the item panel, undo, Ctrl+K search, fit to
// pages and the profile.

import { api, check, openPage, sleep } from "./lib.mjs";

export default async function editor({ base, shots }) {
  const page = await openPage({ shots });
  const state = () => api(base, "GET", "/api/state");
  const stateHas = (cond) => `fetch('/api/state').then((r) => r.json()).then((s) => ${cond})`;
  const panel = 'aside[aria-label="Edit item"]';
  try {
    await page.go(base + "/items/");
    check((await page.js("getComputedStyle(document.querySelector('body > aside')).display")) === "flex", "sidebar navigation on large screens");
    check((await page.js("document.querySelector('h1').textContent.trim()")) === "Items", "page title");

    // The side panel autosaves.
    const target = (await state()).items.find((i) => i.section === "experience");
    const title = target.text.en.title;
    await page.js(`[...document.querySelectorAll('ul button')].find((b) => b.textContent.includes(${JSON.stringify(title)})).click()`);
    await page.until("!!document.querySelector('#title-en')", "side panel");
    check(await page.js(`!!document.querySelector('${panel}') && document.querySelectorAll('ul button').length > 3`), "panel opens next to the list");
    await page.type("#title-en", title + " (autosaved)");
    await page.until(stateHas(`s.items.some((i) => i.text.en.title === ${JSON.stringify(title + " (autosaved)")})`), "autosave", 8000);
    check(true, "item title autosaved without a Save button");
    await page.until("document.body.textContent.includes('Saved ✓')", "saved status", 5000);
    check(true, "Saved ✓ shows in the corner");
    await page.until("!document.body.textContent.includes('Saved ✓') || !document.querySelector('[x-show*=showSaveStatus]').checkVisibility()", "Saved ✓ fades", 6000);
    check(true, "and fades after a few seconds");

    // An invalid year is explained and not saved.
    await page.type("input[aria-label='Start year']", "20");
    await sleep(1200);
    check((await page.js(`document.querySelector('${panel}').textContent`)).includes("four digits"), "invalid year explained");
    check((await state()).items.find((i) => i.id === target.id).start === target.start, "invalid date not saved");
    await page.type("input[aria-label='Start year']", target.start.slice(0, 4));

    // Escape closes the panel and keeps the pending edit.
    await page.type("#title-en", title);
    await page.key("Escape");
    await page.until("!document.querySelector('#title-en')", "panel closed", 5000);
    await sleep(500);
    check((await state()).items.find((i) => i.id === target.id).text.en.title === title, "Escape saved the last edit");

    // A new item in a single-date section, deleted and undone.
    await page.js(`document.querySelector('[aria-label="Add to Publications"]').click()`);
    await page.until("!!document.querySelector('#title-en')", "new item panel");
    check((await page.js("document.querySelector('legend').textContent")).includes("optional"), "publication date is optional");
    await page.type("#title-en", "UI test paper");
    await page.type("#body-en", "Doe, J. (2025). UI test paper. *Journal*.");
    await page.click(`${panel} button`, "Add item");
    await page.until(stateHas("s.items.some((i) => i.text.en.title === 'UI test paper')"), "created", 8000);
    await page.until(`[...document.querySelectorAll('${panel} button')].some((b) => b.textContent.trim() === 'Delete')`, "panel in edit mode", 5000);
    const created = (await state()).items.find((i) => i.text.en.title === "UI test paper");
    check(true, "new publication created: " + created.id);
    await page.js("document.querySelector('[role=tab][aria-selected=false]').click()");
    check((await page.js(`document.querySelector('${panel}').textContent`)).includes("Start from English"), "NL tab offers to start from English");
    await page.click(`${panel} button`, "Delete");
    await page.until("document.body.textContent.includes('Undo')", "undo toast", 5000);
    check(!(await state()).items.some((i) => i.id === created.id), "deleted without a confirm dialog");
    await page.click("button", "Undo");
    await page.until(stateHas(`s.items.some((i) => i.id === ${JSON.stringify(created.id)})`), "restored", 8000);
    check(true, "Undo restored the item under its old id");
    await api(base, "DELETE", `/api/items/publications/${created.id}`);

    // Ctrl+K search jumps to an item.
    await page.go(base + "/");
    await page.key("k", 2); // Ctrl
    await page.until("Alpine.store('cv').paletteOpen", "palette", 3000);
    await page.type("[aria-label='Search'][x-model]", title.split(" ")[0]);
    await sleep(200);
    await page.key("Enter");
    await page.until("location.pathname === '/items/' && !!document.querySelector('#title-en')", "palette opened the item", 10000);
    check((await page.js("document.querySelector('#title-en').value")) === title, "Ctrl+K opened the item in the panel");

    // A version: fits the window; fit to one page.
    await page.go(base + "/v/full-cv/");
    const C = "Alpine.$data(document.querySelector('[x-data=workspace]'))";
    await page.until(`${C} && !${C}.loading && document.querySelectorAll('[x-ref=pages] canvas').length > 0`, "preview", 30000);
    check(await page.js("document.documentElement.scrollHeight <= innerHeight + 1"), "compose fits the window");
    await page.js(C + ".pagesOpen = true");
    await sleep(300);
    await page.click("[aria-label='Fit on'] button", "1");
    await page.until(`!${C}.fitting`, "fit", 60000);
    await sleep(1500);
    await page.until(`!${C}.loading`, "rerender", 30000);
    check(true, "fit to 1 page gave " + (await page.js(`JSON.stringify({ spacing: ${C}.spacing, pages: ${C}.pages })`)));
    await page.shot("compose");

    // An item in a section without any: the section is picked in a dialog,
    // which Escape and a click outside close.
    const picker = "[aria-labelledby=pick-title]";
    const picking = `!!document.querySelector('${picker}').offsetParent`;
    const pick = () => page.js("(() => { const b = [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === '+ Add an item to another section…'); b.focus(); b.click(); })()");
    await pick();
    await page.until(picking, "section dialog", 3000);
    check(await page.js(`!!document.activeElement.closest('${picker}')`), "the section dialog takes the focus");
    await page.key("Escape");
    await page.until(`!${picking}`, "Escape", 3000);
    check(await page.js("document.activeElement.textContent.trim() === '+ Add an item to another section…'"), "Escape closes it, and the focus goes back");
    await pick();
    await page.until(picking, "section dialog", 3000);
    await page.js(`document.querySelector('${picker}').parentElement.click()`);
    await page.until(`!${picking}`, "a click outside", 3000);
    check(true, "a click outside closes it");
    await pick();
    await page.until(picking, "section dialog", 3000);
    const section = await page.js(`document.querySelector('${picker} li button').textContent.trim()`);
    await page.click(`${picker} button`, section);
    await page.until("!!document.querySelector('#title-en')?.offsetParent", "new item panel", 3000);
    check(await page.js(`Alpine.store('cv').sectionName(${C}.form.section) === ${JSON.stringify(section)} && !${picking}`), `picking ${section} starts an item there`);

    // Closing a new item with text in it asks first; Escape answers no.
    const question = "!!document.querySelector('#question-action')?.offsetParent";
    await page.type("#title-en", "Not kept");
    await page.key("Escape");
    await page.until(question, "the discard question", 3000);
    await page.key("Escape");
    await page.until(`!${question}`, "Escape", 3000);
    await sleep(300);
    check(await page.js("document.querySelector('#title-en')?.value === 'Not kept'"), "Escape on the question keeps the new item open");
    await page.key("Escape");
    await page.until(question, "the discard question", 3000);
    await page.click("[role=alertdialog] button", "Discard");
    await page.until("!document.querySelector('#title-en')?.offsetParent", "panel closed", 3000);
    check(!(await state()).items.some((i) => i.text.en?.title === "Not kept"), "Discard closes it unsaved");

    // Profile autosave; a half-typed URL is not saved.
    await page.go(base + "/profile/");
    const before = await state();
    await page.type("#headline-en", "UI test headline");
    await page.until(stateHas("s.profile.text.en.headline === 'UI test headline'"), "profile autosave", 8000);
    check(true, "profile autosaved");
    await page.type("#website", "htt");
    await sleep(1200);
    check((await page.js("document.body.textContent")).includes("Links must start with"), "half-typed URL is not saved, with a reason");
    await page.type("#website", before.profile.website);
    await page.type("#headline-en", before.profile.text.en.headline);
    await sleep(1500);
    await page.shot("profile");

    check(page.dialogs.length === 0, "no dialogs " + JSON.stringify(page.dialogs));
    check(page.errors.length === 0, "no uncaught JS errors " + JSON.stringify(page.errors));
  } finally {
    await page.close();
  }
}
