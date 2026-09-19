/// <reference types="./src/vite-env.d.ts" />
import path from "node:path";
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import { TanStackRouterVite } from "@tanstack/router-plugin/vite";

// The dashboard is served same-origin from the Go backend in production, so
// we default to relative base. The backend dev server (Fiber) is expected to
// either serve the built assets directly or proxy /api/v1/* to itself.
export default defineConfig({
  plugins: [
    TanStackRouterVite({
      routesDirectory: "./src/routes",
      generatedRouteTree: "./src/routeTree.gen.ts",
      autoCodeSplitting: true,
    }),
    react(),
  ],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
      "@api": path.resolve(__dirname, "../api/gen/ts-client/index.ts"),
      "@api-schema": path.resolve(__dirname, "../api/gen/ts/schema.d.ts"),
      // api/gen/ts-client imports openapi-fetch; that file lives outside
      // web/ so Vite/Rollup can't find the dep via the usual node_modules
      // walk. Pin the bare import to web's own copy so the build bundles it.
      "openapi-fetch": path.resolve(__dirname, "./node_modules/openapi-fetch"),
    },
  },
  server: {
    port: 5173,
    strictPort: false,
    proxy: {
      // Forward API + health endpoints to the Go backend during local dev.
      // The dashboard talks to the API same-origin in production; this proxy
      // makes the dev story match.
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: true,
        // ws:true is REQUIRED to proxy the WebSocket upgrade for the
        // WS-32 interactive console endpoint
        // (/api/v1/compute/instances/{id}/console). Without it Vite's
        // http-proxy never binds the 'upgrade' event, so the browser's
        // ws://<vite-host>/api/.../console handshake is answered by
        // Vite's own static server (SPA fallback / 404) and the terminal
        // drops to "disconnected" before a single byte is pumped.
        ws: true,
      },
      "/health": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
  preview: {
    port: 4173,
    strictPort: true,
    // Mirror the dev proxy so `vite preview` (used by the WS-22 e2e harness)
    // routes /api + /health to the running Go backend on :8080. ws:true is
    // required for the WS-32 console WebSocket (see server.proxy note above);
    // the e2e compute spec exercises the same bridge.
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: true,
        ws: true,
      },
      "/health": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
  build: {
    target: "es2022",
    sourcemap: true,
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
      exclude: [
        "src/**/*.test.{ts,tsx}",
        "src/test/**",
        "src/routeTree.gen.ts",
        // Route components are exercised via Playwright e2e (WS-22); the
        // vitest unit surface focuses on pure logic (hooks, lib, format,
        // URL builders). Including the routes in the threshold would
        // drag the floor to ~0% with no actionable signal.
        "src/routes/**",
        // Generated OpenAPI client wrappers — tested via the upstream
        // openapi-fetch project + the WS-22 e2e specs.
        "src/lib/api/**",
      ],
      // WS-22 coverage gate. The MVP aspiration in the WS doc is 60% but
      // the test architecture deliberately pushes most coverage into
      // Playwright e2e specs (WS-22) for the route components and into
      // vitest for pure-logic helpers (hooks, lib, format, URL builders).
      // The thresholds below pin the current baseline so coverage can
      // only go up; raising them is a follow-up tracked in WS-22
      // resolution notes + ADR-0029.
      thresholds: {
        statements: 10,
        branches: 10,
        functions: 10,
        lines: 10,
      },
    },
  },
});
