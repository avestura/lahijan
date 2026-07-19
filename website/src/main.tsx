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

import "@/styles/tailwind.css";

import { initI18n, isRTL } from "@/lib/i18n";
import { applyThemeToDocument, useThemeStore } from "@/lib/stores/theme-store";
import { routes } from "@/routes";

// Sync <html class> with the persisted theme on the very first paint so we
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
  { routes },
  // Setup fn: initialize i18next once before any route mounts. The shared
  // default instance is what react-i18next's useTranslation() reads from,
  // so no provider is required.
  async () => {
    await initI18n();
  },
);
