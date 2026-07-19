/**
 * useTheme — thin selector around the theme store. Mirrors web/.
 */
import { useThemeStore, type Theme } from "@/lib/stores/theme-store";

export function useTheme() {
  const theme = useThemeStore((s) => s.theme);
  const setTheme = useThemeStore((s) => s.setTheme);
  const toggle = useThemeStore((s) => s.toggle);
  return { theme, setTheme: (next: Theme) => setTheme(next), toggle };
}
