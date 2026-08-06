// Reads a design token from :root (tokens.css) at runtime. Chart.js paints to
// a <canvas>, so it cannot resolve var(--x) itself -- colours have to be
// resolved to a concrete string before they reach a chart config.
//
// No colour-literal fallback is passed anywhere in this app: tokens.css is a
// render-blocking <link> in public/dashboard.html, so every token is already
// resolvable by the time any module here is evaluated. Keeping literals out of
// .js is the NR0009 §3.1 token contract. `fallback` stays available for a
// caller that has a non-colour default; unresolved and unbacked returns
// undefined, which makes chart.js fall through to its own default rather than
// paint an invalid colour.
export function cssVar(name, fallback) {
  if (typeof window === "undefined" || typeof getComputedStyle !== "function") {
    return fallback;
  }
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return value || fallback;
}
