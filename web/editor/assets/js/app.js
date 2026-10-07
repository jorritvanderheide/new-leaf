// Editor UI. Every API call answers with the full editor state, so the
// pages just render $store.cv.state and send changes back.

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

async function api(method, path, body) {
  const opts = { method, headers: { "X-CV-App": "1" } };
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

document.addEventListener("alpine:init", () => {
  Alpine.store("cv", {
    state: null,
    error: "",
    notice: "",
    busy: false,

    async load() {
      try {
        this.state = await (await api("GET", "/api/state")).json();
      } catch (e) {
        this.error = e.message;
      }
    },

    async send(method, path, body, notice) {
      this.busy = true;
      this.error = "";
      try {
        this.state = await (await api(method, path, body)).json();
        if (notice) this.flash(notice);
        return true;
      } catch (e) {
        this.error = e.message;
        return false;
      } finally {
        this.busy = false;
      }
    },

    // Opens another CV; the server reads the cookie on every request. Drops
    // the query string, since e.g. ?link= belongs to the previous CV.
    switchUser(user) {
      const secure = location.protocol === "https:" ? "; Secure" : "";
      document.cookie = `cv-user=${encodeURIComponent(user)}; path=/; max-age=31536000; SameSite=Strict${secure}`;
      location.href = location.pathname;
    },

    flash(message) {
      this.notice = message;
      clearTimeout(this._noticeTimer);
      this._noticeTimer = setTimeout(() => (this.notice = ""), 2500);
    },

    async copy(text) {
      try {
        await navigator.clipboard.writeText(text);
        this.flash("Copied to clipboard");
      } catch {
        this.error = "Couldn't copy; select the URL and copy it yourself.";
      }
    },

    langName: (lang) => LANG_NAMES[lang] || lang,
    sectionName: (section) => SECTION_NAMES[section] || section,
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
  });

  Alpine.data("compose", () => ({
    lang: "en",
    photo: true,
    spacing: 1, // whitespace scale for the PDF, 0.4 (tight) to 1.4 (airy)
    selected: [],
    pages: null,
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
    },

    // Called from x-effect whenever the selection changes.
    changed() {
      if (!this.editing) {
        const body = { lang: this.lang, photo: this.photo, spacing: this.spacing, entries: this.selected };
        clearTimeout(this._saveTimer);
        this._saveTimer = setTimeout(() => api("PUT", "/api/compose", body).catch((e) => (this.$store.cv.error = e.message)), 500);
      }
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
        const width = this.$refs.pages.clientWidth;
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

  Alpine.data("items", () => ({
    form: null,

    edit(item) {
      const st = this.$store.cv.state;
      const text = Object.fromEntries(st.langs.map((l) => [l, { title: "", org: "", location: "", body: "", ...item.text[l] }]));
      this.form = { section: item.section, id: item.id, start: item.start, end: item.end, ongoing: !item.end, link: item.link || "", text };
    },

    add(section) {
      this.edit({ section, id: null, start: "", end: "", link: "", text: {} });
    },

    // Fill empty fields of one language from another, e.g. to start a translation.
    copyFrom(from, to) {
      for (const [k, v] of Object.entries(this.form.text[from])) {
        if (!this.form.text[to][k]) this.form.text[to][k] = v;
      }
    },

    async save() {
      const f = this.form;
      const single = this.$store.cv.isPoint(f.section);
      const end = single || f.ongoing ? "" : f.end.trim();
      const body = { section: f.section, start: f.start.trim(), end, link: f.link.trim(), text: f.text };
      const ok = f.id
        ? await this.$store.cv.send("PUT", `/api/items/${f.section}/${f.id}`, body, "Saved")
        : await this.$store.cv.send("POST", "/api/items", body, "Added");
      if (ok) this.form = null;
    },

    async remove() {
      const f = this.form;
      if (!confirm("Delete this item in all languages?")) return;
      if (await this.$store.cv.send("DELETE", `/api/items/${f.section}/${f.id}`, undefined, "Deleted")) this.form = null;
    },
  }));

  Alpine.data("profile", () => ({
    form: null,
    photoVersion: Date.now(),

    init() {
      const st = this.$store.cv.state;
      const p = JSON.parse(JSON.stringify(st.profile)); // structuredClone rejects Alpine proxies
      p.text = Object.fromEntries(st.langs.map((l) => [l, { headline: "", location: "", summary: "", ...p.text[l] }]));
      this.form = p;
    },

    move(i, step) {
      const order = this.form.order;
      [order[i], order[i + step]] = [order[i + step], order[i]];
    },

    save() {
      return this.$store.cv.send("PUT", "/api/profile", this.form, "Profile saved");
    },

    async upload(event) {
      const file = event.target.files[0];
      if (!file) return;
      const data = new FormData();
      data.append("photo", file);
      if (await this.$store.cv.send("POST", "/api/photo", data, "Photo updated")) this.photoVersion = Date.now();
      event.target.value = "";
    },

    async removePhoto() {
      if (confirm("Remove your photo?")) await this.$store.cv.send("DELETE", "/api/photo", undefined, "Photo removed");
    },
  }));

  Alpine.data("links", () => ({
    expiry: {},

    extend(link) {
      const expires = this.expiry[link.slug];
      return this.$store.cv.send("PUT", `/api/links/${link.slug}`, { ...link, expires }, "Expiry updated");
    },

    remove(link) {
      if (!confirm(`Delete "${link.label || link.slug}"? The URL stops working immediately.`)) return;
      return this.$store.cv.send("DELETE", `/api/links/${link.slug}`, undefined, "Link deleted");
    },
  }));
});
