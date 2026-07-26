/**
 * Chart color helpers for the dashboard widgets.
 *
 * The palette is the `--color-chart-N` semantic tokens (5 steps), defined
 * in theme.css and tuned for both light + dark. Reading them at call time
 * via getComputedStyle means a theme switch re-paints the charts without
 * a remount. Callers pass the resolved hex/hsl string straight to recharts.
 */

/** Resolves a CSS variable (e.g. "--color-chart-1") to its current value. */
function cssVar(name: string): string {
  const v = getComputedStyle(document.documentElement).getPropertyValue(name);
  return v.trim() || "hsl(217 91% 60%)";
}

/** Palette of 5 chart colors, in order. */
export function chartPalette(): string[] {
  return [
    cssVar("--color-chart-1"),
    cssVar("--color-chart-2"),
    cssVar("--color-chart-3"),
    cssVar("--color-chart-4"),
    cssVar("--color-chart-5"),
  ];
}

/** Single accent color for a one-series chart. */
export function chartPrimary(): string {
  return cssVar("--color-chart-1");
}
