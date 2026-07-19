/**
 * useTheme — thin selector around the theme store.
 *
 * Exposing theme + setTheme through a hook keeps the consumers decoupled
 * from the store implementation (which lets us swap the persistence layer
 * later without churning call sites).
 */
import { useThemeStore, type Theme } from "@/lib/stores/theme-store";

export function useTheme() {
  const theme = useThemeStore((s) => s.theme);
  const setTheme = useThemeStore((s) => s.setTheme);
  const toggle = useThemeStore((s) => s.toggle);
  return { theme, setTheme: (next: Theme) => setTheme(next), toggle };
}
