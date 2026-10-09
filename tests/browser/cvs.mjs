// Managing CVs in the editor: create, switch, rename, delete. The CV from
// the server configuration (the dev user) can't be deleted.

import { api, check, openPage, sleep } from "./lib.mjs";

export default async function cvs({ base, shots }) {
  const page = await openPage({ shots });
  const dialog = "[x-data=cvs]";
  try {
    await page.go(base + "/");
    const first = await page.js("Alpine.store('cv').state.user");
    await page.js("document.querySelector('body > aside [aria-haspopup=menu]').click()");
    await sleep(300);
    check((await page.js("document.querySelectorAll('body > aside [role=menuitemradio]').length")) === 1, "one CV in the CV menu");
    await page.click("[role=menu] button", "Manage CVs…");
    await page.until(`Alpine.store('cv').manageOpen && document.querySelector('${dialog} > div').offsetParent`, "dialog", 3000);
    check(await page.js(`document.querySelector('${dialog} button[aria-label=Delete]').disabled`), "the only CV can't be deleted");

    await page.type("#new-cv", "Bob Builder");
    await page.type(`${dialog} [aria-label='Owners of the new CV']`, "bob@");
    await page.click(`${dialog} button`, "Create");
    await page.until("Alpine.store('cv').state?.user === 'bob-builder'", "switched to the new CV", 8000);
    check(true, "created a CV and switched to it");
    const st = await page.js("JSON.parse(JSON.stringify(Alpine.store('cv').state))");
    check(st.items.length === 0 && st.cvs.length === 2, "the new CV is empty");
    check(JSON.stringify(st.cvs.find((c) => c.id === "bob-builder").owners) === '["bob@"]', "owners saved");
    await page.go(base + "/v/full-cv/");
    check((await page.js("document.body.textContent")).includes("Start with your first item"), "an empty CV's version shows how to start");

    // A new CV is in English only; a second language is added on Profile.
    check(st.langs.join() === "en", "a new CV is in English only: " + st.langs);
    await page.go(base + "/profile/");
    const tabs = "[...document.querySelectorAll('[role=tablist][aria-label=Language] button')].filter((b) => b.offsetParent).map((b) => b.textContent.trim()).join()";
    check((await page.js(tabs)) === "", "no language tabs with one language");
    const pick = (id, code) => page.js(`(() => { const s = document.querySelector('${id}'); s.value = '${code}'; s.dispatchEvent(new Event('change', { bubbles: true })); })()`);
    await pick("#lang-second", "de");
    await page.until("Alpine.store('cv').state.langs.join() === 'en,de'", "German added", 5000);
    await sleep(300);
    check((await page.js(tabs)) === "English,Deutsch", "a tab per language: " + (await page.js(tabs)));
    // German as the main language swaps them; without English, the Full CV
    // moves to German.
    await pick("#lang-main", "de");
    await page.until("Alpine.store('cv').state.langs.join() === 'de,en'", "German first", 5000);
    await pick("#lang-second", "");
    await page.until("Alpine.store('cv').state.langs.join() === 'de'", "English removed", 5000);
    check((await page.js("Alpine.store('cv').state.versions.find((v) => v.id === 'full-cv').lang")) === "de", "the Full CV moved to German");
    check((await page.js("Alpine.store('cv').toast?.message")).includes("now in Deutsch"), "and the editor says so");
    await page.shot("languages");
    await page.go(base + "/");
    await page.js("document.querySelector('body > aside [aria-haspopup=menu]').click()");
    await sleep(300);
    check((await page.js("[...document.querySelectorAll('body > aside [role=menuitemradio]')].map((o) => o.textContent).join()")).includes("Bob Builder"), "the CV menu shows names");

    // Rename the configured CV; it can't be deleted.
    await page.click("[role=menu] button", "Manage CVs…");
    await page.until(`document.querySelector('${dialog} > div').offsetParent`, "dialog", 3000);
    const firstRow = `[...document.querySelectorAll('${dialog} li')].find((li) => !li.querySelector('span.tag'))`;
    await page.js(`(() => { const i = ${firstRow}.querySelector('input'); i.value = 'Alice (configured)'; i.dispatchEvent(new Event('input', { bubbles: true })); i.dispatchEvent(new Event('change', { bubbles: true })); })()`);
    await page.until(`Alpine.store('cv').state.cvs.some((c) => c.name === 'Alice (configured)')`, "rename", 5000);
    check(true, "renamed a CV");
    check(await page.js(`${firstRow}.querySelector('button[aria-label=Delete]').disabled`), "the configured CV can't be deleted");
    await page.shot("cvs");

    // Delete the open CV: it goes to the trash and the editor opens another.
    // It asks first; Escape answers no, and leaves Manage CVs open.
    const remove = `document.querySelector('${dialog} li span.tag').closest('li').querySelector('button[aria-label=Delete]').click()`;
    const question = "!!document.querySelector('#question-action')?.offsetParent";
    await page.js(remove);
    await page.until(question, "the question", 3000);
    check((await page.js("document.querySelector('#question-title').textContent")).includes("Bob Builder"), "asked before deleting");
    await page.key("Escape");
    await page.until(`!${question}`, "Escape", 3000);
    await sleep(300);
    check(await page.js(`document.querySelector('${dialog}').style.display !== 'none'`), "Escape cancels, and keeps Manage CVs open");
    await page.js(remove);
    await page.until(question, "the question", 3000);
    await page.click("[role=alertdialog] button", "Delete");
    await page.until(`Alpine.store('cv').state?.user === ${JSON.stringify(first)}`, "back on the first CV", 8000);
    check(page.dialogs.length === 0, "no browser dialogs " + JSON.stringify(page.dialogs));
    check((await api(base, "GET", "/api/state")).cvs.length === 1, "deleted");

    await api(base, "PUT", `/api/cvs/${first}`, { name: "", owners: [] });
    check(page.errors.length === 0, "no uncaught JS errors " + JSON.stringify(page.errors));
  } finally {
    await page.close();
  }
}
