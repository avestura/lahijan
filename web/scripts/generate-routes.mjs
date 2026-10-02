// Generates src/routeTree.gen.ts (gitignored) without starting Vite, so
// lint and typecheck work on a fresh checkout. Mirrors the options passed to
// TanStackRouterVite in vite.config.ts.
import { Generator, getConfig } from "@tanstack/router-generator";

const root = process.cwd();
const config = getConfig(
  { routesDirectory: "./src/routes", generatedRouteTree: "./src/routeTree.gen.ts" },
  root,
);
await new Generator({ config, root }).run();
