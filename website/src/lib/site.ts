/**
 * Centralized site configuration for the Lahijan marketing site.
 *
 * All deployer-tunable values (canonical URL, dashboard path, contact
 * address) read from Vite env at build time with safe fallbacks. Components
 * consume the constants here so they don't have to read `import.meta.env`
 * directly.
 */

const RAW_SITE_URL = (import.meta.env.VITE_SITE_URL ?? "http://localhost:4173").trim();
const RAW_DASHBOARD_URL = (import.meta.env.VITE_DASHBOARD_BASE_URL ?? "").trim();

/** Normalize a base URL to its canonical form (no trailing slash). */
function normalizeUrl(url: string): string {
  return url.endsWith("/") ? url.slice(0, -1) : url;
}

/** Canonical site URL (no trailing slash). Used for sitemap + OpenGraph. */
export const SITE_URL = normalizeUrl(RAW_SITE_URL);

/**
 * URL of a Lahijan dashboard, or null when the build has none (the "Sign in"
 * links are hidden then). Absolute URLs and paths on the same host both work.
 */
export const DASHBOARD_URL: string | null = RAW_DASHBOARD_URL || null;

/** Router path of the documentation home. */
export const DOCS_PATH = "/docs";

/** Router path of the quickstart guide. */
export const QUICKSTART_PATH = "/docs/getting-started/quickstart";

/** Router path of the server install guide, the target of "Self-host" calls to action. */
export const INSTALL_PATH = "/docs/getting-started/installation";

/** Source repository. */
export const REPO_URL = "https://github.com/avestura/lahijan";

/** Build version shown in the footer's mono build string. */
export const APP_VERSION: string = __APP_VERSION__;

/** Default OpenGraph image (relative; prerendered as an absolute URL at render time). */
export const DEFAULT_OG_IMAGE = "/favicon.svg";

/**
 * Public navigation entries. The `i18nKey` is the translation key the Header
 * and Footer use to render the label; the route is the path.
 */
export interface NavEntry {
  i18nKey: string;
  route: string;
}

export const NAV_ENTRIES: readonly NavEntry[] = [
  { i18nKey: "nav.features", route: "/features" },
  { i18nKey: "nav.pricing", route: "/pricing" },
  { i18nKey: "nav.docs", route: "/docs" },
  { i18nKey: "nav.blog", route: "/blog" },
  { i18nKey: "nav.about", route: "/about" },
] as const;

/** Footer-only links (legal pages). */
export const LEGAL_ENTRIES: readonly NavEntry[] = [
  { i18nKey: "nav.privacy", route: "/legal/privacy" },
  { i18nKey: "nav.terms", route: "/legal/terms" },
] as const;

/** Build an absolute URL from a path (used for sitemap, OG tags). */
export function absoluteUrl(path: string): string {
  if (path.startsWith("http://") || path.startsWith("https://")) return path;
  const p = path.startsWith("/") ? path : `/${path}`;
  return `${SITE_URL}${p}`;
}
