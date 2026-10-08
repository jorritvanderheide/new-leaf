// Share links: create one from Compose, open the published page, switch
// language, delete it.

import { api, check, openPage } from "./lib.mjs";

export default async function share({ base, shots }) {
  const page = await openPage({ shots });
  try {
    await page.go(base + "/");
    await page.click("button", "Share link");
    await page.until("!!document.querySelector('#share-label').offsetParent", "share dialog", 3000);
    await page.type("#share-label", "Example Job");
    await page.click("form button", "Create link");
    await page.until("document.body.textContent.includes('Link ready')", "link published", 30000);
    const url = await page.js("document.querySelector('input[readonly]').value");
    check(/\/example-job-[a-z0-9]{8}\/$/.test(url), "unguessable URL: " + url);

    await page.send("Page.navigate", { url });
    await page.until("document.readyState === 'complete' && document.body.textContent.includes('Alice Example')", "public page", 10000);
    check((await page.js("document.body.textContent")).includes("Harbour Analytics Lead"), "published page shows the CV");
    check((await page.js("[...document.querySelectorAll('a')].some((a) => a.getAttribute('href') === 'cv.pdf' || a.href.endsWith('/cv.pdf'))")), "with a PDF download");
    await page.shot("share-en");
    await page.js("[...document.querySelectorAll('a')].find((a) => a.getAttribute('href') === 'nl/').click()");
    await page.until("location.pathname.endsWith('/nl/') && document.readyState === 'complete'", "Dutch page", 10000);
    check((await page.js("document.body.textContent")).includes("Havenanalist"), "language toggle opens the Dutch page");
    const pdf = await fetch(url + "cv.pdf");
    check(pdf.ok && (await pdf.arrayBuffer()).byteLength > 1000, "PDF is published");

    const slug = new URL(url).pathname.split("/")[1];
    await api(base, "DELETE", `/api/links/${slug}`);
    check((await fetch(url)).status === 404, "deleted link is gone");
    check(page.errors.length === 0, "no uncaught JS errors " + JSON.stringify(page.errors));
  } finally {
    await page.close();
  }
}
