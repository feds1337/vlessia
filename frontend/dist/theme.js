// Colour scheme and accent colour, applied before the first paint so the
// window does not flash in the wrong colours. The last choice is kept in
// localStorage; app.js calls applyLook with the saved settings.
"use strict";

(() => {
  const KEY = "vlessia-look";
  const ACCENTS = ["lagoon", "blue", "violet", "rose", "orange", "green", "graphite"];
  const dark = window.matchMedia("(prefers-color-scheme: dark)");
  let look = { theme: "", accent: "" };
  try {
    look = { ...look, ...JSON.parse(localStorage.getItem(KEY) || "{}") };
  } catch { /* ignore a broken value */ }

  function paint() {
    const root = document.documentElement;
    const scheme = look.theme === "dark" || look.theme === "light" ? look.theme : (dark.matches ? "dark" : "light");
    root.dataset.scheme = scheme;
    root.dataset.accent = ACCENTS.includes(look.accent) ? look.accent : "lagoon";
  }

  // applyLook sets the theme ("" = system, "light", "dark") and the accent.
  window.applyLook = (theme, accent) => {
    theme = theme || "";
    accent = accent || "";
    if (theme === look.theme && accent === look.accent) return;
    const root = document.documentElement;
    if (!root.classList.contains("no-anim")) {
      root.classList.add("recolor");
      clearTimeout(window.applyLook.timer);
      window.applyLook.timer = setTimeout(() => root.classList.remove("recolor"), 350);
    }
    look = { theme, accent };
    try {
      localStorage.setItem(KEY, JSON.stringify(look));
    } catch { /* storage may be unavailable */ }
    paint();
  };

  dark.addEventListener("change", paint);
  paint();
})();
