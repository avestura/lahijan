/**
 * Vitest + React Testing Library setup.
 *
 * Loaded once per test run via vitest.config.ts `setupFiles`.
 */
import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, beforeAll, vi } from "vitest";
import React from "react";
import type * as ReactRouterDomNS from "react-router-dom";

import { initI18n } from "@/lib/i18n";

// Node 22 ships an experimental global `localStorage` that is gated behind
// the --localstorage-file flag. When accessed without the flag, it warns
// AND returns undefined, shadowing jsdom's implementation. Patch it before
// any module that uses persist is imported.
class MemoryStorage implements Storage {
  private store = new Map<string, string>();
  get length(): number {
    return this.store.size;
  }
  clear(): void {
    this.store.clear();
  }
  getItem(key: string): string | null {
    return this.store.get(key) ?? null;
  }
  key(index: number): string | null {
    return Array.from(this.store.keys())[index] ?? null;
  }
  removeItem(key: string): void {
    this.store.delete(key);
  }
  setItem(key: string, value: string): void {
    this.store.set(key, String(value));
  }
}

Object.defineProperty(globalThis, "localStorage", {
  configurable: true,
  writable: true,
  value: new MemoryStorage(),
});

// react-router-dom's <Link>, <NavLink>, <Outlet>, and hooks need a router
// context. We provide a minimal stub so component tests don't have to
// mount the full router.
vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual<typeof ReactRouterDomNS>("react-router-dom");
  // Strip function-valued props (NavLink's className, style, children-as-
  // function) before handing the rest to a plain <a>. The mock exists only
  // so component tests can render <Link>/<NavLink> without a router.
  function stripFunctions(props: Record<string, unknown>): Record<string, unknown> {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(props)) {
      if (typeof v !== "function") out[k] = v;
    }
    return out;
  }
  return {
    ...actual,
    Link: (props: object & { to?: string }) =>
      React.createElement("a", {
        ...stripFunctions(props as Record<string, unknown>),
        href:
          typeof (props as { to?: string }).to === "string" ? (props as { to?: string }).to : "#",
      }),
    NavLink: (props: object & { to?: string }) =>
      React.createElement("a", {
        ...stripFunctions(props as Record<string, unknown>),
        href:
          typeof (props as { to?: string }).to === "string" ? (props as { to?: string }).to : "#",
      }),
    ScrollRestoration: () => null,
    // Provide a no-op useParams so tests that mount the BlogPostPage work.
    useParams: () => ({}) as Record<string, string | undefined>,
  };
});

beforeAll(async () => {
  // Initialize i18next once with the default locale so useTranslation()
  // returns real translations instead of the i18n keys.
  await initI18n("en");

  // matchMedia is not implemented in jsdom.
  if (typeof window !== "undefined" && !window.matchMedia) {
    Object.defineProperty(window, "matchMedia", {
      writable: true,
      value: (query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addEventListener: () => undefined,
        removeEventListener: () => undefined,
        addListener: () => undefined,
        removeListener: () => undefined,
        dispatchEvent: () => false,
      }),
    });
  }
});

afterEach(() => {
  cleanup();
  try {
    globalThis.localStorage?.clear();
  } catch {
    /* no-op */
  }
});
