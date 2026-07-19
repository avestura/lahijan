/**
 * Centralized site configuration for the Lahijan marketing site.
 *
 * All deployer-tunable values (canonical URL, dashboard path, contact
 * address) read from Vite env at build time with safe fallbacks. Components
 * consume the constants here so they don't have to read `import.meta.env`
 * directly.
 */

const RAW_SITE_URL = (import.meta.env.VITE_SITE_URL ?? "http://localhost:4173").trim();
const RAW_DASHBOARD_BASE_URL = (import.meta.env.VITE_DASHBOARD_BASE_URL ?? "/web/").trim();
const RAW_DOCS_URL = (import.meta.env.VITE_DOCS_URL ?? "/docs/").trim();

/** Normalize a base URL to its canonical form (no trailing slash). */
function normalizeUrl(url: string): string {
  return url.endsWith("/") ? url.slice(0, -1) : url;
}

/** Canonical site URL (no trailing slash). Used for sitemap + OpenGraph. */
export const SITE_URL = normalizeUrl(RAW_SITE_URL);

/** Dashboard base path or URL (always ends in `/`). */
export const DASHBOARD_BASE_URL: string = RAW_DASHBOARD_BASE_URL.endsWith("/")
  ? RAW_DASHBOARD_BASE_URL
  : `${RAW_DASHBOARD_BASE_URL}/`;

/**
 * Docs site base URL (always ends in `/`). Either an absolute URL
 * (`https://docs.lahijan.dev/`) or a sibling-deploy path (`/docs/`). Deep
 * links from the marketing /docs page append a section anchor to this.
 */
export const DOCS_URL: string = RAW_DOCS_URL.endsWith("/") ? RAW_DOCS_URL : `${RAW_DOCS_URL}/`;

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
