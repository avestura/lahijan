/**
 * TanStack Router instance for the dashboard.
 *
 * File-based routing via `@tanstack/router-plugin/vite` regenerates
 * `routeTree.gen.ts` on dev + build. The import below is gitignored; the
 * plugin guarantees its shape.
 */
import { createRouter as _createRouter } from "@tanstack/react-router";

import { routeTree } from "./routeTree.gen";

export function createRouter() {
  return _createRouter({
    routeTree,
    defaultPreload: "intent",
  });
}

export type Router = ReturnType<typeof createRouter>;
