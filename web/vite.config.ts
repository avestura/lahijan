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
      exclude: ["src/**/*.test.{ts,tsx}", "src/test/**", "src/routeTree.gen.ts"],
    },
  },
});
