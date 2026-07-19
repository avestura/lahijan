/// <reference types="vite/client" />

interface ImportMetaEnv {
  /**
   * Base URL of the Lahijan backend. Defaults to the empty string
   * (same-origin) so the production SPA hits the same host that served it.
   * For local dev against a separate backend, set VITE_API_BASE_URL in
   * web/.env.local and the Vite dev server will use it instead of the
   * built-in proxy.
   */
  readonly VITE_API_BASE_URL?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
