/**
 * Theme store tests.
 */
import { describe, expect, it } from "vitest";

import {
  applyThemeToDocument,
  effectiveTheme,
  resolveSystemTheme,
  useThemeStore,
} from "@/lib/stores/theme-store";

describe("theme-store", () => {
  it("resolves system theme against matchMedia", () => {
    // The test setup mocks matchMedia to return `matches: false` so the
    // resolved system theme is "light".
    expect(resolveSystemTheme()).toBe("light");
  });

  it("effectiveTheme passes through light/dark and resolves system", () => {
    expect(effectiveTheme("light")).toBe("light");
    expect(effectiveTheme("dark")).toBe("dark");
    expect(effectiveTheme("system")).toBe("light");
  });

  it("applyThemeToDocument toggles .dark on <html>", () => {
    applyThemeToDocument("dark");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    applyThemeToDocument("light");
    expect(document.documentElement.classList.contains("dark")).toBe(false);
  });

  it("the store's setTheme + toggle keep the document in sync", () => {
    useThemeStore.getState().setTheme("dark");
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    useThemeStore.getState().toggle();
    expect(useThemeStore.getState().theme).toBe("light");
  });
});
