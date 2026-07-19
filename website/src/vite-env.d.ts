/// <reference types="vite/client" />

interface ImportMetaEnv {
  /**
   * Public base URL of the Lahijan deployment. Used to build absolute URLs in
   * sitemap.xml, OpenGraph tags, and the like. Defaults to
   * "http://localhost:4173" for local preview; production deploys must set
   * this to the canonical public URL (e.g. "https://lahijan.dev").
   */
  readonly VITE_SITE_URL?: string;

  /**
   * Base path where the dashboard SPA is mounted. The marketing site's
   * "Sign in" CTA links here. Defaults to "/web/".
   */
  readonly VITE_DASHBOARD_BASE_URL?: string;

  /**
   * Base URL of the separate Docusaurus docs site. The marketing /docs page
   * deep-links here for the full docs. Defaults to "/docs/" (sibling
   * deploy); production deployments override this to the canonical docs
   * URL (e.g. "https://docs.lahijan.dev").
   */
  readonly VITE_DOCS_URL?: string;

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
