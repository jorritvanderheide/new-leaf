// The PDF preview: drawn pages, spacing, and version settings that follow
// the user to another device.

import { check, openPage, sleep } from "./lib.mjs";

const C = "Alpine.$data(document.querySelector('[x-data=workspace]'))";
const drawn = "document.querySelectorAll('[x-ref=pages] canvas').length";

export default async function preview({ base, shots }) {
  const page = await openPage({ shots });
  const spacing = (v) =>
    page.js(`(() => { const s = document.querySelector('#spacing'); s.value = '${v}'; s.dispatchEvent(new Event('input', { bubbles: true })); })()`);
  let selected;
  try {
    await page.go(base + "/v/full-cv/");
    await page.until(`${drawn} > 0 && !${C}.loading`, "preview drawn", 30000);
    const pages = await page.js(`${C}.pages`);
    check((await page.js(drawn)) === pages, `default spacing: ${pages} pages drawn, as the badge says`);
    check((await page.js("document.querySelectorAll('iframe').length")) === 0, "no browser PDF viewer");
    const dark = await page.js(`(() => {
      const c = document.querySelector('[x-ref=pages] canvas');
      const d = c.getContext('2d').getImageData(0, 0, c.width, c.height).data;
      let n = 0;
      for (let i = 0; i < d.length; i += 4) if (d[i] < 128) n++;
      return n;
    })()`);
    check(dark > 1000, `page 1 has text (${dark} dark pixels)`);

    await spacing(0.4);
    await sleep(500);
    await page.until(`!${C}.loading`, "tight preview", 30000);
    const tight = await page.js(`${C}.pages`);
    await spacing(1.4);
    await page.until(`!${C}.loading && ${C}.pages > ${tight}`, "more pages when airy", 30000);
    const airy = await page.js(`${C}.pages`);
    check(airy === (await page.js(drawn)), `airy spacing: ${airy} pages drawn, tight ${tight}`);
    await page.shot("preview-airy");
    await spacing(0.4);
    await page.until(`!${C}.loading && ${C}.pages < ${airy}`, "fewer pages when tight", 30000);
    check(true, `tight spacing: ${await page.js(`${C}.pages`)} pages again`);

    // Settings are kept on the server: another browser gets them.
    await spacing(0.6);
    await page.js("[...document.querySelectorAll('aside[aria-label=Outline] button')].find((b) => b.textContent.trim() === 'None').click()"); // deselects the first section
    await sleep(1500);
    selected = await page.js(`JSON.parse(JSON.stringify(${C}.selected))`);
    check(page.errors.length === 0, "no uncaught JS errors " + JSON.stringify(page.errors));
  } finally {
    await page.close();
  }

  const other = await openPage({ shots });
  try {
    await other.go(base + "/v/full-cv/");
    await other.until(`${C} && !${C}.loading`, "compose ready", 30000);
    check((await other.js(`${C}.spacing`)) === 0.6, "another device opens with the same spacing");
    check(JSON.stringify(await other.js(`JSON.parse(JSON.stringify(${C}.selected))`)) === JSON.stringify(selected), "and the same selection");
    // Select the whole CV again for the next suites.
    await other.js("[...document.querySelectorAll('aside[aria-label=Outline] button')].find((b) => b.textContent.trim() === 'All').click()"); // the section deselected above
    await other.js(`(() => { const s = document.querySelector('#spacing'); s.value = '1'; s.dispatchEvent(new Event('input', { bubbles: true })); })()`);
    await sleep(1500);
  } finally {
    await other.close();
  }
}
