/**
 * Theme store test.
 *
 * Verifies that the persisted theme store flips <html class="dark"> via
 * applyThemeToDocument, and that "system" resolves against the OS
 * preference.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { applyThemeToDocument, resolveSystemTheme, useThemeStore } from "@/lib/stores/theme-store";

describe("theme store", () => {
  beforeEach(() => {
    document.documentElement.classList.remove("dark");
    localStorage.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("applies 'dark' class when setTheme('dark') is called", () => {
    applyThemeToDocument("dark");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(document.documentElement.style.colorScheme).toBe("dark");
  });

  it("removes 'dark' class when setTheme('light') is called", () => {
    applyThemeToDocument("dark");
    applyThemeToDocument("light");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
    expect(document.documentElement.style.colorScheme).toBe("light");
  });

  it("resolves 'system' theme via prefers-color-scheme", () => {
    window.matchMedia = vi.fn().mockImplementation((query: string) => ({
      matches: query.includes("dark"),
      media: query,
      onchange: null,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      addListener: () => undefined,
      removeListener: () => undefined,
      dispatchEvent: () => false,
    })) as unknown as typeof window.matchMedia;
    expect(resolveSystemTheme()).toBe("dark");
    applyThemeToDocument("system");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });

  it("toggle() flips light <-> dark", () => {
    useThemeStore.getState().setTheme("light");
    useThemeStore.getState().toggle();
    expect(useThemeStore.getState().theme).toBe("dark");
    useThemeStore.getState().toggle();
    expect(useThemeStore.getState().theme).toBe("light");
  });
});
