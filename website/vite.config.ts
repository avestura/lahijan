/// <reference types="./src/vite-env.d.ts" />
import { readFileSync } from "node:fs";
import path from "node:path";
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

import { docsPlugin } from "./scripts/docs/plugin";

// The marketing site and its documentation are statically prerendered
// (vite-react-ssg) and shipped behind any static host. The build emits one
// `index.html` per route plus shared assets under dist/. VITE_BASE sets the
// public path prefix, e.g. "/lahijan/" for a GitHub Pages project site.
const pkg = JSON.parse(readFileSync(path.resolve(__dirname, "package.json"), "utf8")) as {
  version: string;
};

const base = normalizeBase(process.env.VITE_BASE ?? "/");

function normalizeBase(b: string): string {
  const withLead = b.startsWith("/") ? b : `/${b}`;
  return withLead.endsWith("/") ? withLead : `${withLead}/`;
}

export default defineConfig({
  base,
  plugins: [docsPlugin(path.resolve(__dirname, "src/content/docs")), react()],
  define: {
    // Build string shown in the footer (see src/lib/site.ts).
    __APP_VERSION__: JSON.stringify(pkg.version),
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 4173,
    strictPort: false,
  },
  preview: {
    port: 4173,
  },
  build: {
    target: "es2022",
    sourcemap: false,
    cssCodeSplit: true,
    rollupOptions: {
      output: {
        // Hash the asset filenames so they can be cached aggressively.
        assetFileNames: "assets/[name].[hash][extname]",
        chunkFileNames: "assets/[name].[hash].js",
        entryFileNames: "assets/[name].[hash].js",
      },
    },
  },
  ssgOptions: {
    // docs/index.html rather than docs.html beside a docs/ folder: static
    // hosts such as GitHub Pages resolve /docs to the folder.
    dirStyle: "nested",
  },
  test: {
    globals: true,
    environment: "jsdom",
    unstubGlobals: true,
    setupFiles: ["./src/test/setup.ts"],
    css: true,
    include: ["src/**/*.{test,spec}.{ts,tsx}"],
    coverage: {
      provider: "v8",
      reporter: ["text", "html"],
      include: ["src/**/*.{ts,tsx}"],
      exclude: ["src/**/*.test.{ts,tsx}", "src/test/**", "src/main.tsx"],
    },
  },
});
