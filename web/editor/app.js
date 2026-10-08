// Editor UI. Every API call answers with the full editor state, so the
// pages just render $store.cv.state and send changes back. Edits save
// automatically; the store tracks that for the save status in the sidebar.

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
// Accent colours to pick from; the first is the default (theme.go).
const ACCENTS = [
  { name: "Green", hex: "#15803d" },
  { name: "Teal", hex: "#00696a" },
  { name: "Blue", hex: "#1d4ed8" },
  { name: "Indigo", hex: "#4f46e5" },
  { name: "Plum", hex: "#86198f" },
  { name: "Crimson", hex: "#b91c1c" },
  { name: "Amber", hex: "#b45309" },
  { name: "Graphite", hex: "#44403c" },
];
const resolveTheme = (t) => ({ accent: (t?.accent || ACCENTS[0].hex).toLowerCase(), font: t?.font || "sans", photo: t?.photo || "rounded" });

// WCAG contrast of a colour against white.
const luminance = (hex) => {
  const [r, g, b] = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255).map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};
const contrastOnWhite = (hex) => 1.05 / (luminance(hex) + 0.05);
const darker = (hex, amount) =>
  "#" + [1, 3, 5].map((i) => Math.round(parseInt(hex.slice(i, i + 2), 16) * (1 - amount)).toString(16).padStart(2, "0")).join("");

// Where a page's content ends, in pt: A4 with the margins of cv.typ.
const PAGE = { top: 45.35, bottom: 799.37, line: 13 };
const PAGES = [
  { label: "Versions", url: "/" },
  { label: "Items", url: "/items/" },
  { label: "Profile", url: "/profile/" },
];

// keepalive lets a request finish while the page is being left, which is how
// pending autosaves are flushed without a "Leave site?" prompt.
async function api(method, path, body, { keepalive = false } = {}) {
  const opts = { method, keepalive, headers: { "X-New-Leaf": "1" } };
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

// focusSoon focuses an element once x-show has shown it, which happens a
// frame or so after the state changes.
window.focusSoon = (el, frames = 10) => {
  if (el.offsetParent) el.focus();
  else if (frames > 0) requestAnimationFrame(() => focusSoon(el, frames - 1));
};

// scrollShadows marks a scroll box's parent with data-above and data-below
// while there is more to scroll that way, for the inner shadows of
// .scroll-shadows (editor.css). Content that grows or shrinks counts too.
window.scrollShadows = (box) => {
  const wrap = box.parentElement;
  const measure = () => {
    wrap.toggleAttribute("data-above", box.scrollTop > 0);
    wrap.toggleAttribute("data-below", box.scrollTop + box.clientHeight < box.scrollHeight - 1);
  };
  box.addEventListener("scroll", measure, { passive: true });
  const resized = new ResizeObserver(measure);
  resized.observe(box);
  for (const child of box.children) resized.observe(child);
};

const itemKey = (item) => `${item.section}/${item.id}`;

// Same order as the CV (sortItems in internal/cv/document.go): ongoing
// first, then by end date; on equal end dates the longer item first.
// Single-date items (no end, point section) sort by their date, undated ones
// last; an undated publication by the year in its reference, e.g. "(2026)".
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

// The item editor (template "item-editor"), for the Items page and the
// version workspace. Existing items save as you type; a new one is created
// with "Add item", after which created(key) is called if there is one.
const itemEditor = () => ({
  form: null,
  tab: "en",
  problem: "", // why the current input can't be saved yet

  initEditor() {
    this._saver = autosaver(this.$store.cv, () => this.persist());
    this.$watch("form", (form) => (this.$store.cv.panelOpen = !!form));
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
    this.tab = (this.lang && !this.$store.cv.missing(item, this.lang) && this.lang) || st.langs.find((l) => !this.$store.cv.missing(item, l)) || st.langs[0];
    this._snapshot = JSON.stringify(this.form);
  },

  add(section) {
    this.edit({ section, id: null, start: "", end: "", link: "", text: {} });
    this.form.present = !this.$store.cv.isPoint(section);
  },

  // addFrom adds an item in the section picked in a select, and resets it.
  addFrom(event) {
    this.add(event.target.value);
    event.target.value = "";
  },

  // Escape closes the item, unless the palette is open over it.
  escape() {
    if (!this.$store.cv.paletteOpen) this.close();
  },

  // The title over the form: the first language that has one.
  formTitle() {
    if (!this.form.id) return "New item";
    return this.$store.cv.state.langs.map((l) => this.form.text[l]?.title).find(Boolean) || "Untitled";
  },

  otherLangs(lang) {
    return this.$store.cv.state.langs.filter((l) => l !== lang);
  },

  // offerCopy: a language without text can start from one with.
  offerCopy(lang, other) {
    return !this.form.text[lang]?.title && !!this.form.text[other]?.title;
  },

  isOpen(item) {
    return this.form && this.form.id === item.id && this.form.section === item.section;
  },

  isOpenKey(key) {
    return this.form && `${this.form.section}/${this.form.id}` === key;
  },

  // itemBody builds the API request, or explains what is missing.
  itemBody() {
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
  itemChanged() {
    const { problem } = this.itemBody();
    // For a new item, only explain once "Add item" was tried.
    this.problem = this.form.id || this._tried ? problem || "" : "";
    if (this.form.id && !problem) this._saver.schedule();
    else this._saver.cancel();
  },

  async persist() {
    const { body } = this.itemBody();
    if (!body || !this.form?.id) return true;
    return this.$store.cv.save("PUT", `/api/items/${this.form.section}/${this.form.id}`, body);
  },

  async create() {
    this._tried = true;
    const { body, problem } = this.itemBody();
    if (problem) return (this.problem = problem);
    const st = this.$store.cv;
    const before = new Set(st.state.items.map(itemKey));
    if (!(await st.send("POST", "/api/items", body, "Item added"))) return;
    const created = st.state.items.find((i) => !before.has(itemKey(i)));
    if (created) {
      this.form.id = created.id;
      this.created?.(itemKey(created));
    }
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
    this.itemChanged();
  },

  async remove() {
    const st = this.$store.cv;
    this._saver.cancel();
    const item = st.state.items.find((i) => i.id === this.form.id && i.section === this.form.section);
    if (!(await st.send("DELETE", `/api/items/${this.form.section}/${this.form.id}`))) return;
    this.form = null;
    const name = st.text(item, st.main()).title;
    st.notify(`Deleted "${name}"`, {
      label: "Undo",
      run: () => st.send("POST", "/api/items", { ...JSON.parse(JSON.stringify(item)) }, "Item restored"),
    });
  },
});

// The workspace's drawn pages, by key: canvases stay out of Alpine's
// reactive data, which would wrap them.
const canvases = new Map();

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
    savedFlash: false, // "Saved" shows for a moment after a save
    panelOpen: false, // the item editor is open

    // Light, dark, or as the system is (mode.js applies it).
    mode: window.cvMode?.get() || "system",
    setMode(mode) {
      this.mode = mode;
      window.cvMode?.set(mode);
    },
    toast: null, // { message, action?: { label, run } }
    paletteOpen: false,
    manageOpen: false,

    // For templates, before the state has loaded too: Alpine's CSP build
    // has no ?. and evaluates both sides of && and ||.
    get cvs() {
      return this.state?.cvs || [];
    },
    get user() {
      return this.state?.user;
    },
    get local() {
      return !!this.state?.local;
    },
    get manage() {
      return !!this.state?.manage;
    },
    get ownersOnly() {
      return !!this.state?.ownersOnly;
    },
    get login() {
      return this.state?.login || "";
    },
    get toastMessage() {
      return this.toast?.message || "";
    },
    get toastLabel() {
      return this.toast?.action?.label || "";
    },
    showSaveStatus() {
      return !!this.state && (this.saveStatus() === "saving" || this.saveStatus() === "error" || this.savedFlash);
    },

    // contentRev counts changes to what a PDF shows (items, profile), so a
    // preview knows to redraw; other state changes leave it alone.
    contentRev: 0,
    setState(st) {
      const content = JSON.stringify([st.items, st.profile]);
      if (this._content !== undefined && content !== this._content) this.contentRev++;
      this._content = content;
      this.state = st;
    },

    async load() {
      try {
        this.setState(await (await api("GET", "/api/state")).json());
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
        this.setState(await (await api(method, path, body)).json());
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
        this.setState(await (await api(method, path, body, { keepalive: true })).json());
        this.saveError = "";
        this.savedAt = Date.now();
        this.savedFlash = true;
        clearTimeout(this._flashTimer);
        this._flashTimer = setTimeout(() => (this.savedFlash = false), 2500);
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

    langName(lang) {
      return this.state?.languages.find((l) => l.code === lang)?.name || lang;
    },

    // The CV's main language: its titles stand for an item in the editor.
    main() {
      return this.state?.langs[0] || "en";
    },

    cvName() {
      return this.state?.cvs.find((cv) => cv.id === this.state.user)?.name || this.state?.user || "";
    },

    // "Alice Example" -> "AE"
    initials() {
      const words = this.cvName().split(/[\s-]+/).filter(Boolean);
      return (words.length > 1 ? words[0][0] + words[words.length - 1][0] : this.cvName().slice(0, 2)).toUpperCase();
    },

    // "2026-11-07" -> "7 Nov 2026"
    day(date) {
      const [y, m, d] = date.split("-");
      return `${+d} ${MONTHS[+m - 1]} ${y}`;
    },

    // An RFC 3339 time as "today", "yesterday", "3 days ago" or a date.
    ago(time) {
      const then = new Date(time);
      const days = Math.round((new Date(this.state.today) - new Date(then.toISOString().slice(0, 10))) / 864e5);
      if (days <= 0) return "today";
      if (days === 1) return "yesterday";
      if (days < 7) return `${days} days ago`;
      return this.day(then.toISOString().slice(0, 10));
    },
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

    focusWhenOpen() {
      if (this.$store.cv.paletteOpen) focusSoon(this.$refs.q);
    },

    results() {
      const st = this.$store.cv.state;
      if (!st) return [];
      const entries = [
        ...PAGES.map((p) => ({ label: p.label, hint: "Page", run: () => (location.href = p.url) })),
        ...st.versions.map((v) => ({ label: v.name, hint: "Version", run: () => (location.href = `/v/${v.id}/`) })),
        ...st.items.map((item) => ({
          label: this.$store.cv.text(item, this.$store.cv.main()).title,
          hint: [this.$store.cv.sectionName(item.section), this.$store.cv.text(item, this.$store.cv.main()).org].filter(Boolean).join(" · "),
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
  // The CV menu: switch CVs, manage them.
  Alpine.data("cvMenu", () => ({
    open: false,

    pick(id) {
      this.open = false;
      if (id !== this.$store.cv.user) this.$store.cv.switchUser(id);
    },

    manage() {
      this.open = false;
      this.$store.cv.manageOpen = true;
    },
  }));

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

  // Items: a list per section, with the item editor on the side.
  Alpine.data("items", () => ({
    ...itemEditor(),

    init() {
      this.initEditor();
      const key = new URLSearchParams(location.search).get("edit");
      if (key) this.openKey(key);
    },
  }));

  // Versions: the overview. Opening one goes to its workspace.
  Alpine.data("versions", () => ({
    creating: false,
    name: "",
    from: "",

    focusWhenCreating() {
      if (this.creating) focusSoon(this.$refs.name);
    },

    // Thumbnails are made in the background after a change: look again
    // until they are all there, for at most half a minute.
    async waitForThumbs() {
      for (let i = 0; i < 20 && this.$store.cv.state.versions.some((v) => !v.thumb); i++) {
        await new Promise((r) => setTimeout(r, 1500));
        try {
          this.$store.cv.setState(await (await api("GET", "/api/state")).json());
        } catch {
          return;
        }
      }
    },

    openNew() {
      Object.assign(this, { creating: true, name: "", from: "" }); // focus: see x-effect
    },

    // make creates a version and returns its ID.
    async make(body) {
      const st = this.$store.cv;
      const before = new Set(st.state.versions.map((v) => v.id));
      if (!(await st.send("POST", "/api/versions", body))) return null;
      return st.state.versions.find((v) => !before.has(v.id))?.id;
    },

    async create() {
      const id = await this.make({ name: this.name, from: this.from });
      if (id) location.href = `/v/${id}/`;
    },

    async duplicate(v) {
      const id = await this.make({ from: v.id });
      if (id) this.$store.cv.notify(`Made “Copy of ${v.name}”`, { label: "Open", run: () => (location.href = `/v/${id}/`) });
    },

    async remove(v) {
      const st = this.$store.cv;
      // A link can't come back at the same address, so that gets a question
      // rather than an undo.
      if (v.link && !v.link.expired && !confirm(`Delete “${v.name}”? Its share link stops working.`)) return;
      const copy = JSON.parse(JSON.stringify(v));
      delete copy.link;
      if (!(await st.send("DELETE", `/api/versions/${v.id}`))) return;
      st.notify(`Deleted “${v.name}”`, { label: "Undo", run: () => st.send("POST", "/api/versions", copy, "Version restored") });
    },
  }));

  // Workspace: one version. Its settings save as they change; the preview
  // redraws when they or the CV's content change, and its items open in the
  // item editor.
  Alpine.data("workspace", () => ({
    ...itemEditor(),
    id: "",
    name: "",
    lang: "en",
    photo: true,
    spacing: 1, // whitespace scale for the PDF, 0.4 (tight) to 1.4 (airy)
    selected: [],
    order: [], // sections, in this version's order
    theme: { accent: "", font: "", photo: "" }, // empty: the default
    lookOpen: false,
    pagesOpen: false,
    accents: ACCENTS,
    pages: null,
    fitPages: 2,
    fitting: false,
    view: "items", // phones show the outline or the preview
    announcement: "", // read out by screen readers, e.g. where a section moved
    pdfUrl: "",
    loading: false,
    pageList: [], // drawn pages: { key, h (pt), marks }
    hover: null, // item key under the pointer, in the outline or the preview
    grab: null, // section whose handle is held
    dragging: null, // section being dragged
    share: { open: false, expires: "", forever: false },
    undoStack: [], // settings to go back to, as snapshot() strings
    redoStack: [],

    init() {
      this.initEditor();
      const st = this.$store.cv.state;
      this.id = location.pathname.split("/")[2];
      const v = this.version();
      if (!v) return void (location.href = "/");
      const existing = new Set(st.items.map(itemKey));
      Object.assign(this, {
        name: v.name,
        lang: v.lang,
        photo: v.photo,
        spacing: v.spacing || 1,
        selected: v.entries.filter((k) => existing.has(k)),
        order: [...(v.order?.length ? v.order : st.profile.order)],
        theme: { accent: "", font: "", photo: "", ...v.theme },
        pages: v.pages || null,
        fitPages: v.fit || 2,
      });
      this._savedPages = v.pages;
      this._versionSaver = autosaver(this.$store.cv, () =>
        this.$store.cv.save("PUT", `/api/versions/${this.id}`, {
          name: this.name,
          lang: this.lang,
          photo: this.photo,
          spacing: this.spacing,
          entries: this.selected,
          order: this.order,
          theme: this.theme,
          pages: (this._savedPages = this.pages || 0),
          fit: this.fitPages,
        }),
      );
      document.title = `${v.name} · New Leaf`;
      this.$watch("name", (name) => {
        document.title = `${name} · New Leaf`;
        this._versionSaver.schedule();
      });
      this.$watch(() => this.$store.cv.contentRev, () => this.rerender());
      this.$watch("fitPages", () => this._versionSaver.schedule());
      const key = new URLSearchParams(location.search).get("edit");
      if (key) this.openKey(key);
    },

    version() {
      return this.$store.cv.state.versions.find((v) => v.id === this.id);
    },

    link() {
      return this.version()?.link;
    },

    linkExpired() {
      return !!this.link()?.expired;
    },

    // The share form would save what the link already has.
    shareUnchanged() {
      return this.shared() && this.shareExpires() === this.link().expires;
    },

    // Escape closes the item, unless the palette or the share form is open.
    escape() {
      if (!this.$store.cv.paletteOpen && !this.share.open) this.close();
    },

    // Read by x-effect: a change to any of these settings is saved.
    watchSettings() {
      void [this.lang, this.photo, this.spacing, this.selected.join(), this.order.join(), this.theme.accent, this.theme.font, this.theme.photo];
      this.settingsChanged();
    },

    missingTitles() {
      return this.missing()
        .map((i) => this.$store.cv.text(i, this.$store.cv.main()).title)
        .join("\n");
    },

    // The sections offered next to a job and education, for a first item.
    extraSections() {
      return this.order.filter((s) => s !== "experience" && s !== "education");
    },

    // show switches what a phone shows: the outline or the preview.
    show(view) {
      this.view = view;
      if (view === "preview") this.resized();
      scrollTo(0, 0);
    },

    customAccent() {
      return !this.accents.some((c) => c.hex === this.resolvedTheme().accent);
    },

    // Called from x-effect when a setting changes, and once on load, when
    // there is nothing to save yet. Only the snapshot is taken inside the
    // effect: what follows reads and writes state, which would run it again.
    settingsChanged() {
      const now = this.snapshot();
      queueMicrotask(() => {
        if (this._loaded) {
          this._versionSaver.schedule();
          this.remember(now);
        }
        this._snap = now;
        this._loaded = true;
        this.rerender();
      });
    },

    // What undo restores: the version's settings. Not its name, which as a
    // text field has an undo of its own.
    snapshot() {
      return JSON.stringify({ lang: this.lang, photo: this.photo, spacing: this.spacing, selected: this.selected, order: this.order, theme: this.theme });
    },

    // remember keeps the settings from before a change. Changes in quick
    // succession, like one drag or one slide, are one step.
    remember(now) {
      if (now === this._snap) return; // an undo or redo itself
      if (!this._grouping) {
        this.undoStack.push(this._snap);
        this.redoStack = [];
      }
      this._grouping = true;
      clearTimeout(this._groupTimer);
      this._groupTimer = setTimeout(() => (this._grouping = !!this.dragging), 800);
    },

    undo() {
      this.step(this.undoStack, this.redoStack);
    },

    redo() {
      this.step(this.redoStack, this.undoStack);
    },

    step(from, to) {
      if (!from.length) return;
      clearTimeout(this._groupTimer);
      this._grouping = false;
      to.push(this._snap);
      this._snap = from.pop();
      Object.assign(this, JSON.parse(this._snap));
    },

    // ⌘Z / Ctrl+Z, and with Shift to redo; text fields keep their own.
    undoKey(event) {
      if (!(event.metaKey || event.ctrlKey) || event.key.toLowerCase() !== "z") return;
      if (event.target.closest?.("textarea, select, input:not([type=checkbox]):not([type=range])")) return;
      event.preventDefault();
      if (event.shiftKey) this.redo();
      else this.undo();
    },

    // A new item from the outline goes on this version.
    created(key) {
      if (!this.selected.includes(key)) this.selected.push(key);
    },

    rerender() {
      clearTimeout(this._timer);
      this._timer = setTimeout(() => this.render(), 350);
    },

    async render() {
      this._abort?.abort();
      const abort = (this._abort = new AbortController());
      const post = (path) =>
        fetch(path, {
          method: "POST",
          headers: { "X-New-Leaf": "1", "Content-Type": "application/json" },
          body: JSON.stringify({ lang: this.lang, entries: this.selected, photo: this.photo, spacing: this.spacing, order: this.order, theme: this.theme }),
          signal: abort.signal,
        });
      this.loading = true;
      try {
        // Where the items are is only for clicking them; the preview works without.
        const [res, marks] = await Promise.all([post("/api/pdf"), post("/api/layout").then((r) => (r.ok ? r.json() : []), () => [])]);
        if (!res.ok) throw new Error((await res.text()).trim());
        const blob = await res.blob();
        await this.draw(blob, marks, abort);
        if (abort.signal.aborted) return;
        if (this.pdfUrl) URL.revokeObjectURL(this.pdfUrl);
        this.pdfUrl = URL.createObjectURL(blob);
        this.pages = +res.headers.get("X-Page-Count");
        if (this.pages !== this._savedPages) this._versionSaver.schedule(); // for the overview
      } catch (e) {
        if (e.name !== "AbortError") this.$store.cv.error = e.message;
      } finally {
        if (this._abort === abort) this.loading = false;
      }
    },

    // Draws the PDF as pages with pdf.js rather than the browser's PDF viewer,
    // which brings its own toolbar. Pages swap in only once all are drawn.
    async draw(blob, marks, abort) {
      this._blob = blob;
      this._marks = marks;
      const pdfjs = await loadPdfjs();
      // No eval for fonts: the editor's CSP forbids it.
      const task = pdfjs.getDocument({ data: new Uint8Array(await blob.arrayBuffer()), isEvalSupported: false });
      try {
        const doc = await task.promise;
        // Hidden behind the phone tab the preview has no width yet: draw at
        // a fixed size (it scales with CSS) and redraw sharp when shown.
        const width = this.$refs.pages.clientWidth || 800;
        const dpr = window.devicePixelRatio || 1;
        const drawn = [];
        for (let n = 1; n <= doc.numPages; n++) {
          const page = await doc.getPage(n);
          const size = page.getViewport({ scale: 1 }); // in pt
          const viewport = page.getViewport({ scale: width / size.width });
          const canvas = document.createElement("canvas");
          canvas.width = Math.floor(viewport.width * dpr);
          canvas.height = Math.floor(viewport.height * dpr);
          canvas.className = "block w-full";
          canvas.setAttribute("role", "img");
          canvas.setAttribute("aria-label", `Page ${n} of ${doc.numPages}`);
          await page.render({ canvas, viewport, transform: [dpr, 0, 0, dpr, 0, 0] }).promise;
          drawn.push({ canvas, h: size.height });
        }
        if (abort?.signal.aborted) return;
        const round = (this._round = (this._round || 0) + 1);
        canvases.clear();
        this.pageList = drawn.map(({ canvas, h }, i) => {
          const key = `${round}-${i}`;
          canvases.set(key, canvas);
          return { key, h, marks: marks.filter((m) => m.page === i + 1) };
        });
      } finally {
        task.destroy();
      }
    },

    canvas(p) {
      return canvases.get(p.key);
    },

    // Redraw at the new width after the window is resized.
    resized() {
      clearTimeout(this._resizeTimer);
      this._resizeTimer = setTimeout(() => this._blob && this.draw(this._blob, this._marks), 200);
    },

    // An item's clickable area on its page, with a little room around it. An
    // object, which Alpine sets property by property: the CSP blocks style
    // attributes.
    markStyle(m, p) {
      const pct = (pt) => `${(pt / p.h) * 100}%`;
      return { top: pct(m.top - 4), height: pct(m.height + 8) };
    },

    markTitle(key) {
      const item = this.$store.cv.state.items.find((i) => itemKey(i) === key);
      return item ? this.itemTitle(item) : "";
    },

    // An item's title in this version's language, for labels.
    itemTitle(item) {
      return this.$store.cv.text(item, this.lang).title || "Untitled item";
    },

    hide(key) {
      this.selected = this.selected.filter((k) => k !== key);
      this.hover = null;
    },

    // Sections with items, in this version's order; the rest can get one.
    shownSections() {
      return this.order.filter((s) => this.$store.cv.items(s).length);
    },

    emptySections() {
      return this.order.filter((s) => !this.$store.cv.items(s).length);
    },

    dragStart(event, section) {
      this.dragging = section;
      event.dataTransfer.effectAllowed = "move";
      event.dataTransfer.setData("text/plain", section);
    },

    // Moves the dragged section to where it is held, as it is dragged.
    dragOver(section) {
      if (!this.dragging || section === this.dragging) return;
      const from = this.order.indexOf(this.dragging);
      const to = this.order.indexOf(section);
      const order = this.order.filter((s) => s !== this.dragging);
      order.splice(from < to ? order.indexOf(section) + 1 : order.indexOf(section), 0, this.dragging);
      this.order = order;
    },

    dragEnd() {
      this._grouping = false; // the drag was one step
      this.dragging = null;
      this.grab = null;
    },

    // Finds the most generous spacing that still fits on fitPages pages.
    async fit() {
      this.fitting = true;
      try {
        const res = await api("POST", "/api/fit", {
          lang: this.lang,
          entries: this.selected,
          photo: this.photo,
          order: this.order,
          theme: this.theme,
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

    resolvedTheme() {
      return resolveTheme(this.theme);
    },

    // The profile holds what new versions start with: a look (with the
    // spacing) and an order.
    isDefault() {
      const p = this.$store.cv.state.profile;
      return JSON.stringify(resolveTheme(p.theme)) === JSON.stringify(this.resolvedTheme()) && (p.spacing || 1) === this.spacing && p.order.join() === this.order.join();
    },

    makeDefault() {
      const profile = JSON.parse(JSON.stringify(this.$store.cv.state.profile));
      return this.$store.cv.send("PUT", "/api/profile", { ...profile, theme: { ...this.theme }, spacing: this.spacing, order: [...this.order] }, "New versions start like this one");
    },

    accentContrast() {
      return contrastOnWhite(this.resolvedTheme().accent);
    },

    // The same colour, darkened until text in it is readable on white.
    darkenAccent() {
      const base = this.resolvedTheme().accent;
      let amount = 0;
      while (contrastOnWhite(darker(base, amount)) < 4.5 && amount < 1) amount += 0.05;
      this.theme.accent = darker(base, amount);
    },

    fitTo(n) {
      this.fitPages = n;
      this.pagesOpen = false;
      return this.fit();
    },

    // How far the content is from the page count to fit on, in lines, from
    // where Typst put the items.
    fitHint() {
      if (!this.pages || !this.pageList.length || this.loading) return "";
      const target = this.fitPages;
      const end = (p) => Math.max(PAGE.top, ...p.marks.map((m) => m.top + m.height));
      const lines = (pt) => Math.max(1, Math.round(pt / PAGE.line));
      if (this.pageList.length > target) {
        const over = this.pageList.slice(target).reduce((sum, p) => sum + end(p) - PAGE.top, 0);
        return `~${lines(over)} lines over ${target === 1 ? "1 page" : target + " pages"}`;
      }
      const last = this.pageList[this.pageList.length - 1];
      const room = PAGE.bottom - end(last) + (target - this.pageList.length) * (PAGE.bottom - PAGE.top);
      return room < PAGE.line ? "" : `Room for ~${lines(room)} more lines`;
    },

    // Where pages start, for the outline: at a section (its first item is
    // on a new page) or at an item within one.
    breaks() {
      const page = {};
      for (const p of this.pageList) for (const m of p.marks) page[m.id] = m.page;
      const out = { sections: {}, items: {} };
      let last = 1;
      for (const section of this.shownSections()) {
        let first = true;
        for (const item of this.$store.cv.items(section)) {
          const n = page[itemKey(item)];
          if (!n) continue;
          if (n > last) (first ? out.sections : out.items)[first ? section : itemKey(item)] = n;
          last = n;
          first = false;
        }
      }
      return out;
    },

    // Alt+↑/↓ on a section's handle; the handle keeps the focus.
    moveSection(section, step) {
      const shown = this.shownSections();
      const other = shown[shown.indexOf(section) + step];
      if (!other) return;
      const order = [...this.order];
      const a = order.indexOf(section);
      const b = order.indexOf(other);
      [order[a], order[b]] = [order[b], order[a]];
      this.order = order;
      // The handle keeps focus, and a screen reader hears where it went.
      this.announcement = `${this.$store.cv.sectionName(section)}: moved to place ${shown.indexOf(other) + 1} of ${shown.length}`;
      this.$nextTick(() => document.querySelector(`[data-handle="${section}"]`)?.focus());
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

    shared() {
      return !!this.link() && !this.link().expired;
    },

    // The expiry to save: a date, or none.
    shareExpires() {
      return this.share.forever ? "" : this.share.expires;
    },

    openShare() {
      const link = this.shared() && this.link();
      this.share = {
        open: true,
        forever: !!link && !link.expires,
        expires: link?.expires || addDays(this.$store.cv.state.today, 30),
      };
    },

    // Shares the version; the dialog then shows the link.
    async saveShare() {
      const st = this.$store.cv;
      const had = this.shared();
      if (!(await this._versionSaver.flush())) return;
      await st.send("PUT", `/api/versions/${this.id}/share`, { expires: this.shareExpires() }, had ? "Saved" : "");
    },

    async unshare() {
      if (!confirm("Stop sharing? The link stops working; sharing again gives a new address.")) return;
      if (await this.$store.cv.send("DELETE", `/api/versions/${this.id}/share`, undefined, "No longer shared")) this.share.open = false;
    },
  }));

  Alpine.data("profile", () => ({
    form: null,
    tab: "en",
    photoVersion: Date.now(),

    init() {
      this.reset();
      this._saver = autosaver(this.$store.cv, () => this.$store.cv.save("PUT", "/api/profile", this.form));
    },

    // reset fills the form from the saved profile, with a tab per language.
    reset() {
      const st = this.$store.cv.state;
      const p = JSON.parse(JSON.stringify(st.profile)); // structuredClone rejects Alpine proxies
      p.text = Object.fromEntries(st.langs.map((l) => [l, { headline: "", location: "", summary: "", ...p.text[l] }]));
      this.form = p;
      this.tab = st.langs[0];
    },

    // pickLang sets the main language (i = 0) or the second one (i = 1, ""
    // for none) right away, outside the autosaved form, whose tabs follow the
    // languages. Picking the second language as the main one swaps them.
    async pickLang(i, code, select) {
      const st = this.$store.cv;
      const before = [...st.state.langs];
      const langs = i === 0 && before[1] === code ? [code, before[0]] : Object.assign([...before], { [i]: code });
      const next = langs.filter(Boolean);
      const moved = st.state.versions.filter((v) => !next.includes(v.lang)).length;
      await st.flushAll();
      const profile = JSON.parse(JSON.stringify(st.state.profile)); // with the text of languages it had before, to bring back
      const notice = moved ? `Languages saved; ${moved === 1 ? "1 version is" : moved + " versions are"} now in ${st.langName(next[0])}` : "Languages saved";
      if (await st.send("PUT", "/api/profile", { ...profile, langs: next }, notice)) this.reset();
      else select.value = before[i] || "";
    },

    // The form's languages: one tab each.
    textLangs() {
      return Object.keys(this.form.text);
    },

    removeLink(i) {
      this.form.links.splice(i, 1);
      this.changed();
    },

    // The languages a CV can have next to its main one.
    secondLangOptions() {
      const main = this.$store.cv.state.langs[0];
      return this.$store.cv.state.languages.filter((l) => l.code !== main);
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

    resumeURL(lang) {
      return "/api/resume?lang=" + lang;
    },

    resumeLabel(lang) {
      const st = this.$store.cv;
      return st.state.langs.length > 1 ? "Download in " + st.langName(lang) : "Download JSON Resume";
    },

    async importResume(event) {
      const st = this.$store.cv;
      const file = event.target.files[0];
      event.target.value = "";
      const links = st.state.sharing ? " Its share links stop working." : "";
      if (!file || !confirm(`Replace your current CV with this resume? A copy of the current CV is kept.${links}`)) return;
      await st.flushAll();
      const data = new FormData();
      data.append("resume", file);
      st.busy = true;
      st.error = "";
      try {
        const { skipped } = await (await api("POST", "/api/resume", data)).json();
        if (skipped.length) alert("Imported. New Leaf has no place for:\n\n- " + skipped.join("\n- "));
        // Reload so every form shows the imported CV.
        location.reload();
      } catch (e) {
        st.error = e.message;
      } finally {
        st.busy = false;
      }
    },

    async removePhoto() {
      if (confirm("Remove your photo? This can't be undone.")) await this.$store.cv.send("DELETE", "/api/photo", undefined, "Photo removed");
    },
  }));
});
