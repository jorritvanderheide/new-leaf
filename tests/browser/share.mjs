// Sharing a version: the link opens a published page in every language,
// and stops working when sharing stops.

import { check, openPage } from "./lib.mjs";

export default async function share({ base, shots }) {
  const page = await openPage({ shots });
  try {
    await page.go(base + "/v/full-cv/");
    await page.click("button", "Share");
    await page.until("!!document.querySelector('[x-model=\"share.forever\"]')?.offsetParent", "share dialog", 3000);
    await page.click("form button", "Create link");
    await page.until("!!document.querySelector('input[aria-label=Link]')?.value", "link published", 30000);
    const url = await page.js("document.querySelector('input[aria-label=Link]').value");
    check(/\/[a-z2-7]{12}\/$/.test(url), "unguessable URL: " + url);
    await page.shot("share-dialog");
    await page.click("form button", "Done");
    await page.go(base + "/");
    check((await page.js("document.body.textContent")).includes("Shared until"), "the overview shows it is shared");

    await page.send("Page.navigate", { url });
    await page.until("document.readyState === 'complete' && document.body.textContent.includes('Alice Example')", "public page", 10000);
    check((await page.js("document.body.textContent")).includes("Harbour Analytics Lead"), "published page shows the CV");
    check(await page.js("[...document.querySelectorAll('a')].some((a) => a.getAttribute('href') === 'cv.pdf')"), "with a PDF download");
    await page.shot("share-en");
    const shown = "[...document.querySelectorAll('.cv-lang')].filter((b) => b.checkVisibility()).map((b) => b.id).join()";
    check((await page.js(shown)) === "en", "only English shows");
    await page.js("document.querySelector('#en a[href=\"#nl\"]').click()");
    await page.until(`${shown} === 'nl'`, "Dutch", 5000);
    check(await page.js("location.hash === '#nl' && document.getElementById('nl').innerText.includes('Havenanalist')"), "the language toggle shows the Dutch CV, on the same page");
    await page.shot("share-nl");
    const pdf = await fetch(url + "cv.pdf");
    check(pdf.ok && (await pdf.arrayBuffer()).byteLength > 1000, "PDF is published");
    const nlPdf = await fetch(url + "nl/cv.pdf");
    check(nlPdf.ok && (await nlPdf.arrayBuffer()).byteLength > 1000, "and the Dutch PDF");
    await page.send("Page.navigate", { url: url + "nl/" });
    await page.until(`location.pathname.endsWith('${new URL(url).pathname}') && location.hash === '#nl' && ${shown} === 'nl'`, "forwarded", 10000);
    check(true, "the old Dutch address forwards to the Dutch CV");

    // No end date: the link stays until sharing stops.
    await page.go(base + "/v/full-cv/");
    await page.click("button", "Shared");
    await page.until("!!document.querySelector('[x-model=\"share.forever\"]')?.offsetParent", "share dialog", 3000);
    await page.js("document.querySelector('form [x-model=\"share.forever\"]').click()");
    await page.click("form button", "Save");
    await page.until("Alpine.store('cv').state.versions.find((v) => v.id === 'full-cv').link?.expires === ''", "no end date", 10000);
    check(true, "shared without an end date");
    await page.send("Page.navigate", { url });
    await page.until("document.readyState === 'complete' && document.body.textContent.includes('Alice Example')", "public page", 10000);
    check(!(await page.js("document.body.textContent")).includes("expires on"), "the public page names no end date");
    await page.go(base + "/");
    check((await page.js("[...document.querySelectorAll('article')].find((a) => a.textContent.includes('Full CV')).textContent")).match(/Shared(?! until)/), "the overview says Shared, without a date");
    await page.go(base + "/v/full-cv/");
    await page.click("button", "Shared");
    await page.until("!!document.querySelector('[x-model=\"share.forever\"]')?.offsetParent", "share dialog", 3000);
    await page.click("form button", "Stop sharing");
    await page.until("!Alpine.store('cv').state.versions[0].link", "unshared", 10000);
    check(page.dialogs.length === 1, "asked before stopping");
    check((await fetch(url)).status === 404, "the link is gone");
    check(page.errors.length === 0, "no uncaught JS errors " + JSON.stringify(page.errors));
  } finally {
    await page.close();
  }
}
