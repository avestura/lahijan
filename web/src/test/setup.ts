/**
 * Vitest + React Testing Library setup.
 *
 * Loaded once per test run via vitest.config.ts `setupFiles`.
 */
import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, beforeAll, vi } from "vitest";
import { vi as vitestVi, type Mock } from "vitest";
import * as React from "react";
import type { ReactNode } from "react";

import i18n from "@/lib/i18n";

// Node 22 ships an experimental global `localStorage` that is gated behind
// the --localstorage-file flag. When accessed without the flag, it warns
// AND returns undefined, shadowing jsdom's implementation. Zustand's
// persist middleware captures `localStorage` once at module load, so we
// must patch it BEFORE any module that uses persist is imported.
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

// Replace the broken Node 22 localStorage global at the very top of setup,
// before any test-owned module captures a reference to it.
Object.defineProperty(globalThis, "localStorage", {
  configurable: true,
  writable: true,
  value: new MemoryStorage(),
});

// TanStack Router's Link component uses useNavigate under the head, which
// needs a router context. We provide a tiny stub so component tests don't
// have to mount the full router.
vi.mock("@tanstack/react-router", () => {
  return {
    Link: (props: React.AnchorHTMLAttributes<HTMLAnchorElement>) => React.createElement("a", props),
    useNavigate: () =>
      vitestVi.fn(async () => {
        /* no-op */
      }) as unknown as Mock<(opts: { to: string }) => Promise<void>>,
    redirect: (opts: { to: string }) => opts,
  };
});

void i18n;

beforeAll(() => {
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

// Re-export for tests that need to wrap components in providers.
export { type ReactNode };
