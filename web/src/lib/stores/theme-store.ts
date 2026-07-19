/**
 * UI-only theme store.
 *
 * The theme is `light` | `dark` | `system`. When `system`, the effective
 * value is resolved against `prefers-color-scheme: dark` and re-resolves
 * automatically when the user changes their OS preference.
 *
 * The store persists to localStorage so reloads keep the user's pick. The
 * <html class> + media-query subscriptions are wired in
 * `applyThemeToDocument`, which is called from main.tsx on bootstrap and on
 * every store change.
 *
 * Note (per web/AGENTS.md): Zustand is for UI-only state. Auth/user state
 * does NOT live here — that's the session store.
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
 * applyThemeToDocument flips <html class="dark"> for the active theme.
 * The Tailwind config (darkMode: ["class"]) reads this attribute.
 */
export function applyThemeToDocument(theme: Theme): EffectiveTheme {
  const eff = effectiveTheme(theme);
  if (typeof document === "undefined") return eff;
  const root = document.documentElement;
  root.classList.toggle("dark", eff === "dark");
  root.style.colorScheme = eff;
  return eff;
}

export const useThemeStore = create<ThemeState>()(
  persist(
    (set, get) => ({
      theme: "system",
      setTheme: (next) => {
        set({ theme: next });
        applyThemeToDocument(next);
        // Subscribe to OS-level changes when "system".
        if (next === "system" && typeof window !== "undefined") {
          const mq = window.matchMedia("(prefers-color-scheme: dark)");
          mq.addEventListener("change", () => applyThemeToDocument("system"));
        }
      },
      toggle: () => {
        const next: Theme = get().theme === "dark" ? "light" : "dark";
        set({ theme: next });
        applyThemeToDocument(next);
      },
    }),
    {
      name: STORAGE_KEY,
      // Only persist the user's choice; setters come from the store factory.
      partialize: (s) => ({ theme: s.theme }),
      // After rehydration, sync <html> with the persisted theme.
      onRehydrateStorage: () => (state) => {
        if (state) applyThemeToDocument(state.theme);
      },
    },
  ),
);
