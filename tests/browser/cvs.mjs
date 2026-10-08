// Managing CVs in the editor: create, switch, rename, delete. The CV from
// the server configuration (the dev user) can't be deleted.

import { api, check, openPage } from "./lib.mjs";

export default async function cvs({ base, shots }) {
  const page = await openPage({ shots });
  const dialog = "[x-data=cvs]";
  try {
    await page.go(base + "/");
    const first = await page.js("Alpine.store('cv').state.user");
    check((await page.js("document.querySelectorAll('aside select').length")) === 0, "one CV: no switcher");
    await page.click("aside button", "Manage CVs");
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
    check((await page.js("[...document.querySelector('aside select').options].map((o) => o.text).join()")).includes("Bob Builder"), "switcher shows names");

    // Rename the configured CV; it can't be deleted.
    await page.click("aside button", "Manage");
    await page.until(`document.querySelector('${dialog} > div').offsetParent`, "dialog", 3000);
    const firstRow = `[...document.querySelectorAll('${dialog} li')].find((li) => !li.querySelector('span.rounded-full'))`;
    await page.js(`(() => { const i = ${firstRow}.querySelector('input'); i.value = 'Alice (configured)'; i.dispatchEvent(new Event('input', { bubbles: true })); i.dispatchEvent(new Event('change', { bubbles: true })); })()`);
    await page.until(`Alpine.store('cv').state.cvs.some((c) => c.name === 'Alice (configured)')`, "rename", 5000);
    check(true, "renamed a CV");
    check(await page.js(`${firstRow}.querySelector('button[aria-label=Delete]').disabled`), "the configured CV can't be deleted");
    await page.shot("cvs");

    // Delete the open CV: it goes to the trash and the editor opens another.
    await page.js(`document.querySelector('${dialog} li span.rounded-full').closest('li').querySelector('button[aria-label=Delete]').click()`);
    await page.until(`Alpine.store('cv').state?.user === ${JSON.stringify(first)}`, "back on the first CV", 8000);
    check(page.dialogs.length === 1 && page.dialogs[0].includes("Bob Builder"), "asked before deleting");
    check((await api(base, "GET", "/api/state")).cvs.length === 1, "deleted");

    await api(base, "PUT", `/api/cvs/${first}`, { name: "", owners: [] });
    check(page.errors.length === 0, "no uncaught JS errors " + JSON.stringify(page.errors));
  } finally {
    await page.close();
  }
}
