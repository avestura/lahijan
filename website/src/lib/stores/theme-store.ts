/**
 * UI-only theme store (mirrors web/src/lib/stores/theme-store.ts).
 *
 * The theme is `light` | `dark` | `system`. When `system`, the effective
 * value is resolved against `prefers-color-scheme: dark` and re-resolves
 * automatically when the user changes their OS preference.
 *
 * Persists to localStorage so reloads keep the user's pick. The
 * <html data-theme> attribute (read by the Boxy tokens in boxy.css) is
 * written by `applyThemeToDocument`, called from main.tsx on bootstrap and
 * on every store change. Before JS runs, boxy.css falls back to
 * `prefers-color-scheme`, so the prerendered HTML is already themed.
 */
import { create } from "zustand";
import { persist } from "zustand/middleware";

export type Theme = "light" | "dark" | "system";
export type EffectiveTheme = "light" | "dark";

const STORAGE_KEY = "lahijan.theme";

interface ThemeState {
  theme: Theme;
  setTheme: (next: Theme) => void;
  toggle: () => void;
}

/**
 * systemToEffective resolves the OS preference. Defaults to "light" when
 * `matchMedia` is unavailable (SSR, very old browsers).
 */
export function resolveSystemTheme(): EffectiveTheme {
  if (typeof window === "undefined" || !window.matchMedia) return "light";
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

export function effectiveTheme(theme: Theme): EffectiveTheme {
  return theme === "system" ? resolveSystemTheme() : theme;
}

/**
 * applyThemeToDocument sets <html data-theme="light|dark"> for the active
 * theme. Boxy's role tokens and the Tailwind `dark:` variant both key off
 * that attribute. The legacy `dark` class is kept in sync for any code that
 * still checks it.
 */
export function applyThemeToDocument(theme: Theme): EffectiveTheme {
  const eff = effectiveTheme(theme);
  if (typeof document === "undefined") return eff;
  const root = document.documentElement;
  root.dataset.theme = eff;
  root.classList.toggle("dark", eff === "dark");
  root.style.colorScheme = eff;
  return eff;
}

let systemListenerBound = false;

/**
 * bindSystemThemeListener re-applies the theme when the OS preference
 * changes while the user's pick is "system". Bound once per page.
 */
function bindSystemThemeListener(read: () => Theme): void {
  if (systemListenerBound || typeof window === "undefined" || !window.matchMedia) return;
  systemListenerBound = true;
  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => {
    if (read() === "system") applyThemeToDocument("system");
  });
}

export const useThemeStore = create<ThemeState>()(
  persist(
    (set, get) => ({
      theme: "system",
      setTheme: (next) => {
        set({ theme: next });
        applyThemeToDocument(next);
        bindSystemThemeListener(() => get().theme);
      },
      toggle: () => {
        const next: Theme = get().theme === "dark" ? "light" : "dark";
        set({ theme: next });
        applyThemeToDocument(next);
      },
    }),
    {
      name: STORAGE_KEY,
      partialize: (s) => ({ theme: s.theme }),
      onRehydrateStorage: () => (state) => {
        if (!state) return;
        applyThemeToDocument(state.theme);
        bindSystemThemeListener(() => useThemeStore.getState().theme);
      },
    },
  ),
);
