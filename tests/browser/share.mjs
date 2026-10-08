// Sharing a version: the link opens a published page in every language,
// and stops working when sharing stops.

import { check, openPage } from "./lib.mjs";

export default async function share({ base, shots }) {
  const page = await openPage({ shots });
  try {
    await page.go(base + "/v/full-cv/");
    await page.click("button", "Share");
    await page.until("!!document.querySelector('#share-expires').offsetParent", "share dialog", 3000);
    await page.click("form button", "Create link");
    await page.until("!!document.querySelector('input[aria-label=Link]')?.value", "link published", 30000);
    const url = await page.js("document.querySelector('input[aria-label=Link]').value");
    check(/\/full-cv-[a-z0-9]{8}\/$/.test(url), "unguessable URL: " + url);
    await page.shot("share-dialog");
    await page.click("form button", "Done");
    await page.go(base + "/");
    check((await page.js("document.body.textContent")).includes("Shared until"), "the overview shows it is shared");

    await page.send("Page.navigate", { url });
    await page.until("document.readyState === 'complete' && document.body.textContent.includes('Alice Example')", "public page", 10000);
    check((await page.js("document.body.textContent")).includes("Harbour Analytics Lead"), "published page shows the CV");
    check(await page.js("[...document.querySelectorAll('a')].some((a) => a.getAttribute('href') === 'cv.pdf')"), "with a PDF download");
    await page.shot("share-en");
    await page.js("[...document.querySelectorAll('a')].find((a) => a.getAttribute('href') === 'nl/').click()");
    await page.until("location.pathname.endsWith('/nl/') && document.readyState === 'complete'", "Dutch page", 10000);
    check((await page.js("document.body.textContent")).includes("Havenanalist"), "language toggle opens the Dutch page");
    const pdf = await fetch(url + "cv.pdf");
    check(pdf.ok && (await pdf.arrayBuffer()).byteLength > 1000, "PDF is published");

    await page.go(base + "/v/full-cv/");
    await page.click("button", "Shared");
    await page.until("!!document.querySelector('#share-expires').offsetParent", "share dialog", 3000);
    await page.click("form button", "Stop sharing");
    await page.until("!Alpine.store('cv').state.versions[0].link", "unshared", 10000);
    check(page.dialogs.length === 1, "asked before stopping");
    check((await fetch(url)).status === 404, "the link is gone");
    check(page.errors.length === 0, "no uncaught JS errors " + JSON.stringify(page.errors));
  } finally {
    await page.close();
  }
}
