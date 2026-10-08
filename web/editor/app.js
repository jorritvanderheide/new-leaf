// Editor UI. Every API call answers with the full editor state, so the
// pages just render $store.cv.state and send changes back. Edits save
// automatically; the store tracks that for the save status in the sidebar.

const LANG_NAMES = { en: "English", nl: "Nederlands" };
const SECTION_NAMES = {
  experience: "Work experience",
  education: "Education",
  publications: "Publications",
  output: "Other output",
  presentations: "Presentations",
  teaching: "Teaching",
  awards: "Grants & awards",
  extracurricular: "Extracurricular activities",
  volunteering: "Volunteering",
};
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const PAGES = [
  { label: "Compose", url: "/" },
  { label: "Items", url: "/items/" },
  { label: "Profile", url: "/profile/" },
  { label: "Links", url: "/links/" },
];

// keepalive lets a request finish while the page is being left, which is how
// pending autosaves are flushed without a "Leave site?" prompt.
async function api(method, path, body, { keepalive = false } = {}) {
  const opts = { method, keepalive, headers: { "X-CV-App": "1" } };
  if (body instanceof FormData) {
    opts.body = body;
  } else if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  if (!res.ok) throw new Error((await res.text()).trim() || res.statusText);
  return res;
}

// Alpine rejects a promise nobody awaits when an x-show transition is cut
// short, e.g. a toast replaced while it fades out. Nothing went wrong.
window.addEventListener("unhandledrejection", (e) => {
  if (e.reason?.isFromCancelledTransition) e.preventDefault();
});

const itemKey = (item) => `${item.section}/${item.id}`;

// Same order as the CV (web/cv/layouts/_partials/cv.html): ongoing first, then
// by end date; on equal end dates the longer item first. Single-date items
// (no end, point section) sort by their date, undated ones last; an undated
// publication by the year in its reference, e.g. "(2026)".
const refYear = (i) => Object.values(i.text).map((t) => /\((\d{4})[a-z]?\)/.exec(t.body || "")?.[1]).find(Boolean) || "";
const byRecency = (point, publications) => (a, b) => {
  const start = (i) => i.start || (publications ? refYear(i) : "");
  const end = (i) => (point ? start(i) : i.end || "9999-12");
  return end(b).localeCompare(end(a)) || a.start.localeCompare(b.start);
};

// "2023-09" -> "Sep 2023"; "2024" -> "2024"; "" -> "".
const month = (ym) => {
  const [y, m] = (ym || "").split("-");
  return m ? `${MONTHS[+m - 1]} ${y}` : y || "";
};

// Dates are edited as a month (optional) and a year, stored as YYYY-MM or YYYY.
const splitDate = (d) => {
  const [y = "", m = ""] = (d || "").split("-");
  return { y, m };
};
const joinDate = ({ y, m }) => {
  y = String(y || "").trim();
  return y ? (m ? `${y}-${m}` : y) : "";
};

// pdf.js, loaded on first use; it is only needed on the Compose page.
let pdfjsModule;
const loadPdfjs = () =>
  (pdfjsModule ??= import("/vendor/pdfjs/pdf.min.mjs").then((pdfjs) => {
    pdfjs.GlobalWorkerOptions.workerSrc = "/vendor/pdfjs/pdf.worker.min.mjs";
    return pdfjs;
  }));

const addDays = (date, days) => {
  const d = new Date(`${date}T12:00:00`);
  d.setDate(d.getDate() + days);
  return d.toISOString().slice(0, 10);
};

// Changes waiting to be saved, so ⌘S and leaving the page can deal with them.
const pending = new Set();

// autosaver saves after a pause in editing; flush() saves right away.
function autosaver(store, save, delay = 700) {
  let timer;
  const saver = {
    schedule() {
      clearTimeout(timer);
      pending.add(saver);
      store.dirty = pending.size;
      timer = setTimeout(() => saver.flush(), delay);
    },
    async flush() {
      clearTimeout(timer);
      if (!pending.delete(saver)) return true;
      store.dirty = pending.size;
      return save();
    },
    cancel() {
      clearTimeout(timer);
      pending.delete(saver);
      store.dirty = pending.size;
    },
  };
  return saver;
}

document.addEventListener("alpine:init", () => {
  Alpine.store("cv", {
    state: null,
    error: "",
    busy: false, // an explicit action (create, publish) is running
    saving: 0, // autosaves in flight
    dirty: 0, // edits waiting for their autosave
    saveError: "", // the last autosave failed
    invalid: "", // input that is not saved until fixed
    savedAt: 0,
    toast: null, // { message, action?: { label, run } }
    paletteOpen: false,
    manageOpen: false,

    async load() {
      try {
        this.state = await (await api("GET", "/api/state")).json();
      } catch (e) {
        this.error = e.message;
        return;
      }
      // A local app stops a while after the last editor closed; an open
      // editor tells it so every minute.
      if (this.state.local) setInterval(() => fetch("/api/ping").catch(() => {}), 60000);
    },

    stopped: false,

    // Local only: save, then stop the app.
    async quit() {
      await this.flushAll();
      await api("POST", "/api/quit").catch(() => {});
      this.stopped = true;
    },

    // send performs an explicit action and reports failures loudly.
    async send(method, path, body, notice) {
      this.busy = true;
      this.error = "";
      try {
        this.state = await (await api(method, path, body)).json();
        if (notice) this.notify(notice);
        return true;
      } catch (e) {
        this.error = e.message;
        return false;
      } finally {
        this.busy = false;
      }
    },

    // save is an autosave: progress shows in the save status instead.
    async save(method, path, body) {
      this.saving++;
      try {
        this.state = await (await api(method, path, body, { keepalive: true })).json();
        this.saveError = "";
        this.savedAt = Date.now();
        return true;
      } catch (e) {
        this.saveError = e.message;
        return false;
      } finally {
        this.saving--;
      }
    },

    async flushAll() {
      const results = await Promise.all([...pending].map((s) => s.flush()));
      return results.every(Boolean);
    },

    saveStatus() {
      if (this.invalid || this.saveError) return "error";
      if (this.saving || this.dirty) return "saving";
      return this.savedAt ? "saved" : "";
    },

    // Opens another CV; the server reads the cookie on every request. Drops
    // the query string, since e.g. ?link= belongs to the previous CV.
    async switchUser(user) {
      await this.flushAll();
      const secure = location.protocol === "https:" ? "; Secure" : "";
      document.cookie = `cv-user=${encodeURIComponent(user)}; path=/; max-age=31536000; SameSite=Strict${secure}`;
      location.href = location.pathname;
    },

    notify(message, action = null) {
      this.toast = { message, action };
      clearTimeout(this._toastTimer);
      this._toastTimer = setTimeout(() => (this.toast = null), action ? 7000 : 2500);
    },

    async runToastAction() {
      const action = this.toast?.action;
      this.toast = null;
      await action?.run();
    },

    async copy(text) {
      try {
        await navigator.clipboard.writeText(text);
        this.notify("Copied to clipboard");
      } catch {
        this.error = "Couldn't copy; select the URL and copy it yourself.";
      }
    },

    // Global shortcuts: ⌘K / Ctrl+K search, ⌘S / Ctrl+S save now.
    keys(event) {
      const mod = event.metaKey || event.ctrlKey;
      if (mod && event.key.toLowerCase() === "k") {
        event.preventDefault();
        this.paletteOpen = !this.paletteOpen;
      } else if (mod && event.key.toLowerCase() === "s") {
        event.preventDefault();
        this.flushAll().then((ok) => ok && this.notify("Saved"));
      }
    },

    // Leaving the page sends pending autosaves right away (they survive the
    // unload), so there is nothing to warn about.
    beforeUnload() {
      this.flushAll();
    },

    langName: (lang) => LANG_NAMES[lang] || lang,
    sectionName: (section) => SECTION_NAMES[section] || section,
    months: MONTHS,
    key: itemKey,
    isPoint(section) {
      return this.state.pointSections.includes(section);
    },

    period(item) {
      if (this.isPoint(item.section)) return month(item.start);
      return `${month(item.start)} – ${item.end ? month(item.end) : "Present"}`;
    },

    // Sections in this user's CV order.
    sections() {
      return this.state.profile.order;
    },

    items(section) {
      return this.state.items
        .filter((i) => i.section === section)
        .sort(byRecency(this.isPoint(section), section === "publications"));
    },

    // An item's text in a language, falling back to whichever is filled in.
    text(item, lang) {
      const t = item.text[lang];
      if (t?.title) return t;
      return Object.values(item.text).find((x) => x.title) || { title: "(untitled)", org: "" };
    },

    missing(item, lang) {
      return !item.text[lang]?.title;
    },
  });

  // ⌘K: jump to any page, item or CV.
  Alpine.data("palette", () => ({
    q: "",
    index: 0,

    results() {
      const st = this.$store.cv.state;
      if (!st) return [];
      const entries = [
        ...PAGES.filter((p) => p.url !== "/links/" || st.sharing).map((p) => ({ label: p.label, hint: "Page", run: () => (location.href = p.url) })),
        ...st.items.map((item) => ({
          label: this.$store.cv.text(item, "en").title,
          hint: [this.$store.cv.sectionName(item.section), this.$store.cv.text(item, "en").org].filter(Boolean).join(" · "),
          run: () => this.openItem(item),
        })),
        ...st.cvs
          .filter((cv) => cv.id !== st.user)
          .map((cv) => ({ label: `Open ${cv.name}`, hint: "Switch CV", run: () => this.$store.cv.switchUser(cv.id) })),
        ...(st.manage ? [{ label: "Manage CVs", hint: "New, rename, delete", run: () => (this.$store.cv.manageOpen = true) }] : []),
      ];
      const words = this.q.toLowerCase().split(/\s+/).filter(Boolean);
      return entries.filter((e) => words.every((w) => `${e.label} ${e.hint}`.toLowerCase().includes(w))).slice(0, 12);
    },

    openItem(item) {
      if (location.pathname === "/items/") {
        window.dispatchEvent(new CustomEvent("open-item", { detail: itemKey(item) }));
      } else {
        location.href = `/items/?edit=${encodeURIComponent(itemKey(item))}`;
      }
    },

    run(entry) {
      this.$store.cv.paletteOpen = false;
      this.q = "";
      entry?.run();
    },

    key(event) {
      const n = this.results().length;
      if (event.key === "ArrowDown") this.index = (this.index + 1) % Math.max(n, 1);
      else if (event.key === "ArrowUp") this.index = (this.index - 1 + n) % Math.max(n, 1);
      else if (event.key === "Enter") this.run(this.results()[this.index]);
      else return;
      event.preventDefault();
    },
  }));

  // Manage CVs: create, rename and delete. CVs from the server
  // configuration can be renamed but not deleted.
  Alpine.data("cvs", () => ({
    newName: "",
    newOwners: "",
    logins: (s) => s.split(/[\s,]+/).filter(Boolean),

    save(cv, name, owners) {
      return this.$store.cv.send("PUT", `/api/cvs/${encodeURIComponent(cv.id)}`, { name, owners: this.logins(owners) }, "Saved");
    },

    async create() {
      const store = this.$store.cv;
      store.busy = true;
      store.error = "";
      try {
        const res = await api("POST", "/api/cvs", { name: this.newName, owners: this.logins(this.newOwners) });
        await store.switchUser((await res.json()).id);
      } catch (e) {
        store.error = e.message;
      } finally {
        store.busy = false;
      }
    },

    async remove(cv) {
      const store = this.$store.cv;
      const links = store.state.sharing ? " Its share links stop working." : "";
      const where = store.state.local ? "the trash folder next to your CVs" : "the server's trash folder, where an admin can restore it";
      if (!confirm(`Delete the CV “${cv.name}”?${links} It is moved to ${where}.`)) return;
      try {
        await api("DELETE", `/api/cvs/${encodeURIComponent(cv.id)}`);
      } catch (e) {
        store.error = e.message;
        return;
      }
      if (cv.id === store.state.user) {
        location.href = location.pathname; // the cookie no longer matches: opens the default CV
      } else {
        await store.send("GET", "/api/state", undefined, "CV deleted");
      }
    },
  }));

  Alpine.data("compose", () => ({
    lang: "en",
    photo: true,
    spacing: 1, // whitespace scale for the PDF, 0.4 (tight) to 1.4 (airy)
    selected: [],
    pages: null,
    fitPages: 2,
    fitting: false,
    view: "items", // phones show the item list or the preview

    pdfUrl: "",
    loading: false,
    editing: null, // the link being edited, when opened from the Links page
    share: { open: false, label: "", expires: "", result: null },

    init() {
      const st = this.$store.cv.state;
      const slug = new URLSearchParams(location.search).get("link");
      const link = slug && st.links.find((l) => l.slug === slug);
      // Settings are stored per CV on the server. Older versions kept them in
      // this browser; take those over once if the server has none yet.
      const legacyKey = `cv-compose:${st.user}`;
      const legacy = JSON.parse(localStorage.getItem(legacyKey) || "null");
      localStorage.removeItem(legacyKey);
      const saved = st.compose
        ? { lang: st.compose.lang, photo: st.compose.photo, spacing: st.compose.spacing || 1, selected: st.compose.entries || [] }
        : legacy;
      if (link) {
        this.editing = link;
        Object.assign(this, { lang: link.lang, photo: link.photo, spacing: link.spacing || 1, selected: [...link.entries] });
      } else if (saved) {
        Object.assign(this, saved);
      } else {
        this.selected = st.items.map(itemKey);
      }
      const existing = new Set(st.items.map(itemKey));
      this.selected = this.selected.filter((k) => existing.has(k));
      this._saver = autosaver(this.$store.cv, () =>
        api("PUT", "/api/compose", { lang: this.lang, photo: this.photo, spacing: this.spacing, entries: this.selected }, { keepalive: true })
          .then(() => true)
          .catch((e) => ((this.$store.cv.saveError = e.message), false)),
      );
    },

    // Called from x-effect whenever the selection changes, and once on load,
    // when there is nothing to save yet.
    changed() {
      if (!this.editing && this._loaded) this._saver.schedule();
      this._loaded = true;
      clearTimeout(this._timer);
      this._timer = setTimeout(() => this.render(), 350);
    },

    async render() {
      this._abort?.abort();
      const abort = (this._abort = new AbortController());
      this.loading = true;
      try {
        const res = await fetch("/api/pdf", {
          method: "POST",
          headers: { "X-CV-App": "1", "Content-Type": "application/json" },
          body: JSON.stringify({ lang: this.lang, entries: this.selected, photo: this.photo, spacing: this.spacing }),
          signal: abort.signal,
        });
        if (!res.ok) throw new Error((await res.text()).trim());
        const blob = await res.blob();
        await this.draw(blob, abort);
        if (this.pdfUrl) URL.revokeObjectURL(this.pdfUrl);
        this.pdfUrl = URL.createObjectURL(blob);
        this.pages = +res.headers.get("X-Page-Count");
      } catch (e) {
        if (e.name !== "AbortError") this.$store.cv.error = e.message;
      } finally {
        if (this._abort === abort) this.loading = false;
      }
    },

    // Draws the PDF as pages with pdf.js rather than the browser's PDF viewer,
    // which brings its own toolbar. Pages swap in only once all are drawn.
    async draw(blob, abort) {
      this._blob = blob;
      const pdfjs = await loadPdfjs();
      const task = pdfjs.getDocument({ data: new Uint8Array(await blob.arrayBuffer()) });
      try {
        const doc = await task.promise;
        // Hidden behind the phone tab the preview has no width yet: draw at
        // a fixed size (it scales with CSS) and redraw sharp when shown.
        const width = this.$refs.pages.clientWidth || 800;
        const dpr = window.devicePixelRatio || 1;
        const canvases = [];
        for (let n = 1; n <= doc.numPages; n++) {
          const page = await doc.getPage(n);
          const viewport = page.getViewport({ scale: width / page.getViewport({ scale: 1 }).width });
          const canvas = document.createElement("canvas");
          canvas.width = Math.floor(viewport.width * dpr);
          canvas.height = Math.floor(viewport.height * dpr);
          canvas.className = "block w-full bg-white shadow-md ring-1 ring-stone-900/5";
          canvas.setAttribute("aria-label", `Page ${n} of ${doc.numPages}`);
          await page.render({ canvas, viewport, transform: [dpr, 0, 0, dpr, 0, 0] }).promise;
          canvases.push(canvas);
        }
        if (!abort?.signal.aborted) this.$refs.pages.replaceChildren(...canvases);
      } finally {
        task.destroy();
      }
    },

    // Redraw at the new width after the window is resized.
    resized() {
      clearTimeout(this._resizeTimer);
      this._resizeTimer = setTimeout(() => this._blob && this.draw(this._blob), 200);
    },

    // Finds the most generous spacing that still fits on fitPages pages.
    async fit() {
      this.fitting = true;
      try {
        const res = await api("POST", "/api/fit", {
          lang: this.lang,
          entries: this.selected,
          photo: this.photo,
          pages: this.fitPages,
        });
        const fit = await res.json();
        this.spacing = fit.spacing;
        if (!fit.fits) {
          this.$store.cv.notify(`Doesn't fit on ${this.fitPages} ${this.fitPages === 1 ? "page" : "pages"} even at the tightest spacing (${fit.pages} pages). Deselect some items.`);
        }
      } catch (e) {
        this.$store.cv.error = e.message;
      } finally {
        this.fitting = false;
      }
    },

    // Selected items without text in the current language.
    missing() {
      const selected = new Set(this.selected);
      return this.$store.cv.state.items.filter((i) => selected.has(itemKey(i)) && this.$store.cv.missing(i, this.lang));
    },

    toggleSection(section, on) {
      const keys = this.$store.cv.items(section).map(itemKey);
      this.selected = on ? [...new Set([...this.selected, ...keys])] : this.selected.filter((k) => !keys.includes(k));
    },

    sectionState(section) {
      const keys = this.$store.cv.items(section).map(itemKey);
      const n = keys.filter((k) => this.selected.includes(k)).length;
      return n === 0 ? "none" : n === keys.length ? "all" : "some";
    },

    filename() {
      const name = this.$store.cv.state.profile.name || "CV";
      return `CV ${name} (${this.lang.toUpperCase()}).pdf`;
    },

    openShare() {
      const today = this.$store.cv.state.today;
      this.share = {
        open: true,
        label: this.editing?.label || "",
        expires: this.editing?.expires || addDays(today, 30),
        result: null,
      };
    },

    async saveLink() {
      const st = this.$store.cv;
      const body = {
        label: this.share.label,
        lang: this.lang,
        entries: this.selected,
        photo: this.photo,
        spacing: this.spacing,
        expires: this.share.expires,
      };
      const before = new Set(st.state.links.map((l) => l.slug));
      const ok = this.editing
        ? await st.send("PUT", `/api/links/${this.editing.slug}`, body)
        : await st.send("POST", "/api/links", body);
      if (!ok) return;
      const link = this.editing
        ? st.state.links.find((l) => l.slug === this.editing.slug)
        : st.state.links.find((l) => !before.has(l.slug));
      this.editing = this.editing && link;
      this.share.result = link;
    },
  }));

  // Items: a list per section, and a side panel that edits one item. Existing
  // items save as you type; a new one is created with "Add item".
  Alpine.data("items", () => ({
    form: null,
    tab: "en",
    problem: "", // why the current input can't be saved yet

    init() {
      this._saver = autosaver(this.$store.cv, () => this.persist());
      const key = new URLSearchParams(location.search).get("edit");
      if (key) this.openKey(key);
    },

    openKey(key) {
      const item = this.$store.cv.state.items.find((i) => itemKey(i) === key);
      if (item) this.edit(item);
    },

    async edit(item) {
      if (this.form) await this._saver.flush();
      const st = this.$store.cv.state;
      const text = Object.fromEntries(st.langs.map((l) => [l, { title: "", org: "", location: "", body: "", ...item.text[l] }]));
      this.form = {
        section: item.section,
        id: item.id,
        start: splitDate(item.start),
        end: splitDate(item.end),
        present: !item.end,
        link: item.link || "",
        text,
      };
      this.problem = "";
      this._tried = false;
      this.tab = st.langs.find((l) => this.$store.cv.missing(item, l) === false) || st.langs[0];
      this._snapshot = JSON.stringify(this.form);
    },

    add(section) {
      this.edit({ section, id: null, start: "", end: "", link: "", text: {} });
      this.form.present = !this.$store.cv.isPoint(section);
    },

    isOpen(item) {
      return this.form && this.form.id === item.id && this.form.section === item.section;
    },

    // body builds the API request, or explains what is missing.
    body() {
      const f = this.form;
      const point = this.$store.cv.isPoint(f.section);
      for (const [name, d] of [["Start", f.start], ["End", f.end]]) {
        if (d.y && !/^\d{4}$/.test(String(d.y).trim())) return { problem: `${name} year must have four digits.` };
        if (d.m && !String(d.y).trim()) return { problem: `${name} needs a year.` };
      }
      const start = joinDate(f.start);
      const end = point || f.present ? "" : joinDate(f.end);
      if (!point && !start) return { problem: "Add a start year." };
      if (!point && !f.present && !end) return { problem: "Add an end year, or mark it as present." };
      if (end && end < start) return { problem: "The end is before the start." };
      return { body: { section: f.section, start, end, link: f.link.trim(), text: f.text } };
    },

    // Called on every edit in the panel.
    changed() {
      const { problem } = this.body();
      // For a new item, only explain once "Add item" was tried.
      this.problem = this.form.id || this._tried ? problem || "" : "";
      if (this.form.id && !problem) this._saver.schedule();
      else this._saver.cancel();
    },

    async persist() {
      const { body } = this.body();
      if (!body || !this.form?.id) return true;
      return this.$store.cv.save("PUT", `/api/items/${this.form.section}/${this.form.id}`, body);
    },

    async create() {
      this._tried = true;
      const { body, problem } = this.body();
      if (problem) return (this.problem = problem);
      const st = this.$store.cv;
      const before = new Set(st.state.items.map(itemKey));
      if (!(await st.send("POST", "/api/items", body, "Item added"))) return;
      const created = st.state.items.find((i) => !before.has(itemKey(i)));
      if (created) this.form.id = created.id;
      this._snapshot = JSON.stringify(this.form);
    },

    async close() {
      if (!this.form) return;
      if (this.form.id) {
        await this._saver.flush();
      } else if (JSON.stringify(this.form) !== this._snapshot && !confirm("Discard this new item?")) {
        return;
      }
      this.form = null;
    },

    copyFrom(from, to) {
      for (const [k, v] of Object.entries(this.form.text[from])) {
        if (!this.form.text[to][k]) this.form.text[to][k] = v;
      }
      this.changed();
    },

    async remove() {
      const st = this.$store.cv;
      this._saver.cancel();
      const item = st.state.items.find((i) => i.id === this.form.id && i.section === this.form.section);
      if (!(await st.send("DELETE", `/api/items/${this.form.section}/${this.form.id}`))) return;
      this.form = null;
      const name = st.text(item, "en").title;
      st.notify(`Deleted "${name}"`, {
        label: "Undo",
        run: () => st.send("POST", "/api/items", { ...JSON.parse(JSON.stringify(item)) }, "Item restored"),
      });
    },
  }));

  Alpine.data("profile", () => ({
    form: null,
    tab: "en",
    photoVersion: Date.now(),

    init() {
      const st = this.$store.cv.state;
      const p = JSON.parse(JSON.stringify(st.profile)); // structuredClone rejects Alpine proxies
      p.text = Object.fromEntries(st.langs.map((l) => [l, { headline: "", location: "", summary: "", ...p.text[l] }]));
      this.form = p;
      this.tab = st.langs[0];
      this._saver = autosaver(this.$store.cv, () => this.$store.cv.save("PUT", "/api/profile", this.form));
    },

    // Waits with saving while a link is still being typed, since the server
    // would reject it.
    changed() {
      const urls = [this.form.website, ...this.form.links.map((l) => l.url)].map((u) => (u || "").trim());
      if (urls.some((u) => u && !/^https?:\/\/\S+$/.test(u))) {
        this._saver.cancel();
        this.$store.cv.invalid = "Links must start with http:// or https://";
        return;
      }
      this.$store.cv.invalid = "";
      this._saver.schedule();
    },

    // A language tab is flagged when another language has text it lacks.
    incomplete(lang) {
      const t = this.form.text[lang];
      return Object.values(this.form.text).some((o) => o !== t && ["headline", "location", "summary"].some((k) => o[k] && !t[k]));
    },

    move(i, step) {
      const order = this.form.order;
      [order[i], order[i + step]] = [order[i + step], order[i]];
      this.changed();
    },

    async upload(event) {
      const file = event.target.files[0];
      if (!file) return;
      const data = new FormData();
      data.append("photo", file);
      if (await this.$store.cv.send("POST", "/api/photo", data, "Photo updated")) this.photoVersion = Date.now();
      event.target.value = "";
    },

    async restore(event) {
      const file = event.target.files[0];
      event.target.value = "";
      if (!file || !confirm("Replace your current CV with this backup? A copy of the current CV is kept.")) return;
      await this.$store.cv.flushAll();
      const data = new FormData();
      data.append("backup", file);
      // Reload so every form shows the restored CV.
      if (await this.$store.cv.send("POST", "/api/import", data)) location.reload();
    },

    async removePhoto() {
      if (confirm("Remove your photo? This can't be undone.")) await this.$store.cv.send("DELETE", "/api/photo", undefined, "Photo removed");
    },
  }));

  Alpine.data("links", () => ({
    expiry: {},

    extend(link) {
      const expires = this.expiry[link.slug];
      return this.$store.cv.send("PUT", `/api/links/${link.slug}`, { ...link, expires }, "Expiry updated");
    },

    async remove(link) {
      const st = this.$store.cv;
      // An expired link can't be re-published as it was, so it gets no undo.
      if (link.expired && !confirm(`Delete "${link.label || link.slug}"?`)) return;
      if (!(await st.send("DELETE", `/api/links/${link.slug}`))) return;
      const copy = JSON.parse(JSON.stringify(link));
      if (link.expired) return st.notify("Link deleted");
      st.notify(`Deleted "${link.label || link.slug}"; the URL no longer works`, {
        label: "Undo",
        run: () => st.send("POST", "/api/links", copy, "Link restored"),
      });
    },
  }));
});
