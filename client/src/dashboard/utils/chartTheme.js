// Single point where the dashboard's chart.js instances pick up tokens.css.
//
// The three chart components each register chart.js separately, so without
// this module each one would carry its own copy of the palette -- exactly the
// "every screen brings its own colours" drift NR0009 §3.1 forbids. Every value
// below comes from tokens.css through cssVar(); this file holds no literals.
import { cssVar } from "@/dashboard/utils/cssVar";

/**
 * Global chart.js defaults, applied once per chart module at import time.
 * chart.js ships light-theme defaults (near-black text, a 10%-black grid) that
 * are invisible on the deck's dark `.tile` surface.
 */
export function applyChartDefaults(Chart) {
  Chart.defaults.color = cssVar("--muted");            // legend + tick labels
  Chart.defaults.borderColor = cssVar("--line-soft");  // scale grid lines
  Chart.defaults.font.family = cssVar("--font-sans");
}

/**
 * Status palette for chart series. The keys are the status words the screens
 * already use, and each colour is the same token components.css gives the
 * matching `.badge` modifier -- so a bar and the badge for the same status are
 * literally the same colour.
 *
 *   active   -> .badge.online    inactive -> .badge.offline
 *   error    -> .badge.danger    wait     -> .badge.warning
 */
export function chartPalette() {
  return {
    active: cssVar("--green"),
    error: cssVar("--red"),
    inactive: cssVar("--neutral"),
    running: cssVar("--blue"),
    wait: cssVar("--amber"),
    completed: cssVar("--green"),
    // Doughnut segment gap: the tile surface. chart.js defaults this to white,
    // which draws bright seams across a dark panel.
    surface: cssVar("--panel"),
  };
}
