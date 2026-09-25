/**
 * Marketing SPA entrypoint.
 *
 * Wires react-i18next (single instance shared by SSG + client), applies the
 * persisted theme to <html> before first paint (no flash), and exports the
 * `createApp` factory vite-react-ssg drives both at SSG build time and at
 * client hydration time.
 *
 * Per ADR-0028, the marketing site uses react-router-dom + vite-react-ssg
 * (the dashboard in web/ stays on TanStack Router). The deviation is scoped
 * to this directory.
 *
 * Per-page <head> tags (title, description, OpenGraph, Twitter card,
 * canonical, hreflang) are emitted by <SeoHead> via <Head> at SSG build
 * time, so they appear in the prerendered HTML.
 */
import { ViteReactSSG } from "vite-react-ssg";

// Boxy tokens first (the token file), then the self-hosted fonts, then the
// Tailwind layers that consume both.
import "@/styles/boxy.css";
import "@fontsource-variable/inter";
import "@fontsource-variable/inter-tight";
import "@fontsource-variable/jetbrains-mono";
import "@fontsource-variable/vazirmatn";
import "@/styles/tailwind.css";
import "@/styles/docs.css";

import { initI18n, isRTL } from "@/lib/i18n";
import { applyThemeToDocument, useThemeStore } from "@/lib/stores/theme-store";
import { routes } from "@/routes";

// Sync <html data-theme> (and the legacy .dark class) with the persisted theme on the very first paint so we
// don't get a flash of the wrong theme. Skipped during SSG (no document).
if (typeof document !== "undefined") {
  applyThemeToDocument(useThemeStore.getState().theme);
}

// Sync <html lang/dir> with the persisted locale (or the browser default).
// Skipped during SSG (we render the default locale at build time).
if (typeof document !== "undefined") {
  const initialLocale = localStorage.getItem("lahijan.locale")?.split(/[-_]/)[0] ?? "en";
  document.documentElement.lang = initialLocale;
  document.documentElement.dir = isRTL(initialLocale) ? "rtl" : "ltr";
}

export const createRoot = ViteReactSSG(
  // basename follows Vite's base (VITE_BASE), so the site works both at the
  // domain root and under a path such as a GitHub Pages project site.
  { routes, basename: import.meta.env.BASE_URL },
  // Setup fn: initialize i18next once before any route mounts. The shared
  // default instance is what react-i18next's useTranslation() reads from,
  // so no provider is required.
  async () => {
    await initI18n();
  },
);
