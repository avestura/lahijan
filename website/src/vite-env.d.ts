/// <reference types="vite/client" />

/** Package version, injected by vite.config.ts `define`. */
declare const __APP_VERSION__: string;

interface ImportMetaEnv {
  /**
   * Public base URL of the Lahijan deployment. Used to build absolute URLs in
   * sitemap.xml, OpenGraph tags, and the like. Defaults to
   * "http://localhost:4173" for local preview; production deploys must set
   * this to the canonical public URL (e.g. "https://lahijan.dev").
   */
  readonly VITE_SITE_URL?: string;

  /**
   * Optional URL of a Lahijan dashboard. When set, the header and footer show
   * a "Sign in" link to it. Leave unset for a site that only presents the
   * project (for example the GitHub Pages build).
   */
  readonly VITE_DASHBOARD_BASE_URL?: string;

  /**
   * Optional Plausible domain (e.g. "lahijan.dev"). When set, the Plausible
   * script is injected at runtime. Leave unset to disable analytics.
   */
  readonly VITE_PLAUSIBLE_DOMAIN?: string;

  /**
   * Optional Google Analytics measurement ID (e.g. "G-XXXX"). When set, the
   * GA tag is injected at runtime. Leave unset to disable analytics.
   */
  readonly VITE_GA_MEASUREMENT_ID?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

/** A documentation page compiled by scripts/docs/plugin.ts. */
declare module "*.md" {
  const doc: {
    title: string;
    description: string;
    html: string;
    headings: { id: string; text: string; depth: 2 | 3 }[];
  };
  export default doc;
}

/** Search data for every docs page (scripts/docs/plugin.ts). */
declare module "virtual:docs-index" {
  const index: {
    slug: string;
    title: string;
    description: string;
    headings: string[];
    text: string;
  }[];
  export default index;
}
