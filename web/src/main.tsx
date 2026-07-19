/**
 * Dashboard SPA entrypoint.
 *
 * Bootstraps:
 *   1. i18next (loads en/fa from the bundle).
 *   2. The theme store + applies it to <html>.
 *   3. TanStack Query + TanStack Router.
 *   4. React root.
 *
 * The bootstrap also kicks off the /auth/me session check by reading the
 * session store once (the useAuth hook in the auth-guarded layouts drives
 * the actual query).
 */
import React from "react";
import ReactDOM from "react-dom/client";
import { RouterProvider } from "@tanstack/react-router";

import "@/styles/tailwind.css";

import { applyThemeToDocument, useThemeStore } from "@/lib/stores/theme-store";
import "@/lib/i18n";
import { isRTL } from "@/lib/i18n";
import { useSessionStore } from "@/lib/stores/session-store";
import { createRouter } from "./router";

// Sync <html class> with the persisted theme on the very first paint so we
// don't get a flash of the wrong theme.
applyThemeToDocument(useThemeStore.getState().theme);

// Sync <html lang/dir> with the persisted locale (or the browser default).
const initialLocale = localStorage.getItem("lahijan.locale")?.split(/[-_]/)[0] ?? "en";
if (typeof document !== "undefined") {
  document.documentElement.lang = initialLocale;
  document.documentElement.dir = isRTL(initialLocale) ? "rtl" : "ltr";
}

// Mark the session as loading until /auth/me resolves; the auth layout will
// react when the bootstrap query lands.
useSessionStore.getState().setStatus("loading");

const router = createRouter();

const rootEl = document.getElementById("root");
if (!rootEl) {
  throw new Error("Root element #root not found");
}

ReactDOM.createRoot(rootEl).render(
  <React.StrictMode>
    <RouterProvider router={router} />
  </React.StrictMode>,
);
