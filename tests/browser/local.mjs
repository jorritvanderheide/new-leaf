// Local mode (cv-app without "serve"): versions without sharing, backups,
// Quit.

import { check, openPage } from "./lib.mjs";

export default async function local({ base, shots }) {
  const page = await openPage({ shots });
  try {
    await page.go(base + "/v/full-cv/");
    const nav = await page.js("JSON.stringify([...document.querySelectorAll('body > aside nav a')].map((a) => a.textContent.trim()))");
    check(nav === JSON.stringify(["Versions", "Items", "Profile"]), "sidebar: " + nav);
    check((await page.js("[...document.querySelectorAll('button')].filter((b) => b.textContent.trim() === 'Share' && b.offsetParent).length")) === 0, "no Share button");
    await page.until("document.querySelectorAll('[x-ref=pages] canvas').length > 0", "preview of the empty CV", 20000);
    check(true, "preview renders");
    await page.shot("local");

    await page.go(base + "/profile/");
    check((await page.js("document.body.textContent")).includes("Download backup"), "backup card on Profile");
    const res = await fetch(base + "/api/export");
    check(res.ok && res.headers.get("content-type") === "application/zip", "backup downloads as zip");

    await page.click("button", "Quit");
    await page.until("Alpine.store('cv').stopped", "stopped screen", 5000);
    let stopped = false;
    for (let i = 0; i < 20 && !stopped; i++) {
      await new Promise((r) => setTimeout(r, 250));
      stopped = await fetch(base + "/api/ping").then(() => false, () => true);
    }
    check(stopped, "Quit stopped the app");
    check(page.errors.length === 0, "no uncaught JS errors " + JSON.stringify(page.errors));
  } finally {
    await page.close();
  }
}
