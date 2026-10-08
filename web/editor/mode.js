// Light or dark, applied before the page paints so it never flashes: the
// choice made in this browser, or else the system's. Loaded without defer.
(() => {
  const system = matchMedia("(prefers-color-scheme: dark)");
  const apply = () => {
    const mode = localStorage.getItem("cv-mode") || "system";
    document.documentElement.classList.toggle("dark", mode === "dark" || (mode === "system" && system.matches));
  };
  apply();
  system.addEventListener("change", apply);
  window.cvMode = {
    get: () => localStorage.getItem("cv-mode") || "system",
    set(mode) {
      if (mode === "system") localStorage.removeItem("cv-mode");
      else localStorage.setItem("cv-mode", mode);
      apply();
    },
  };
})();
