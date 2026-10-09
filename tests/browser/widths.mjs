// A version at phone, tablet and desktop widths: nothing overflows sideways.

import { check, openPage, sleep } from "./lib.mjs";

export default async function widths({ base, shots }) {
  const page = await openPage({ shots });
  try {
    for (const [w, h] of [[390, 844], [768, 1024], [1024, 760], [1280, 800], [1440, 900]]) {
      await page.viewport(w, h);
      await page.go(base + "/v/full-cv/");
      await sleep(800);
      if (w < 1024) {
        check(await page.js("document.documentElement.scrollWidth <= innerWidth"), `${w}px: no sideways overflow on the Items tab`);
        await page.shot(`width-${w}-items`);
        await page.js("[...document.querySelectorAll('[role=tab]')].find((b) => b.textContent.trim().startsWith('Preview')).click()");
      }
      await page.until("document.querySelectorAll('[x-ref=pages] canvas').length > 0 && document.querySelector('[x-ref=pages]').clientWidth > 0", "preview", 30000);
      await sleep(1200);
      check(await page.js("document.documentElement.scrollWidth <= innerWidth"), `${w}px: no sideways overflow`);
      const overflow = await page.js(`(() => {
        const bar = document.querySelector('[data-settings]');
        const r = bar.getBoundingClientRect();
        return [...bar.children].some((c) => c.getBoundingClientRect().right > r.right + 1);
      })()`);
      check(!overflow, `${w}px: settings fit inside their bar`);
      await page.shot(`width-${w}`);
    }
    check(page.errors.length === 0, "no uncaught JS errors " + JSON.stringify(page.errors));
  } finally {
    await page.close();
  }
}
