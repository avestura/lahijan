/**
 * Analytics bootstrap.
 *
 * Supports an optional, deployer-configured analytics provider. Either:
 *   - Plausible (set `VITE_PLAUSIBLE_DOMAIN`), or
 *   - Google Analytics 4 (set `VITE_GA_MEASUREMENT_ID`).
 *
 * If neither env is set, this is a no-op (no script is injected; the
 * marketing site is analytics-free). The hook is called once on the client
 * after hydration; it is a no-op during SSG.
 */
import { useEffect } from "react";

const PLAUSIBLE_SCRIPT_SRC = "https://plausible.io/js/script.js";
const GA_SCRIPT_SRC = "https://www.googletagmanager.com/gtag/js";

interface AnalyticsGlobals {
  plausible?: (event: string, options?: { props?: Record<string, string> }) => void;
  dataLayer?: unknown[];
  gtag?: (...args: unknown[]) => void;
}

function getGlobals(): AnalyticsGlobals {
  return typeof window !== "undefined" ? (window as unknown as AnalyticsGlobals) : {};
}

function injectScript(src: string, attrs: Record<string, string> = {}): HTMLScriptElement {
  const el = document.createElement("script");
  el.src = src;
  el.async = true;
  for (const [k, v] of Object.entries(attrs)) {
    el.setAttribute(k, v);
  }
  document.head.appendChild(el);
  return el;
}

/**
 * useAnalytics injects the appropriate analytics script(s) on first client
 * mount. Safe to call from any component; it is idempotent.
 */
export function useAnalytics(): void {
  useEffect(() => {
    if (typeof window === "undefined") return;

    const plausible = import.meta.env.VITE_PLAUSIBLE_DOMAIN;
    const ga = import.meta.env.VITE_GA_MEASUREMENT_ID;

    if (plausible && !document.querySelector(`script[src="${PLAUSIBLE_SCRIPT_SRC}"]`)) {
      injectScript(PLAUSIBLE_SCRIPT_SRC);
    }

    if (ga && !document.querySelector(`script[src="${GA_SCRIPT_SRC}"]`)) {
      injectScript(`${GA_SCRIPT_SRC}?id=${ga}`);
      const w = getGlobals();
      w.dataLayer = w.dataLayer ?? [];
      w.gtag = function (...args: unknown[]) {
        const g = getGlobals();
        g.dataLayer?.push(args);
      };
      w.gtag("js", new Date());
      w.gtag("config", ga);
    }
  }, []);
}
