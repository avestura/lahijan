/**
 * ThemeToggle — light / dark / system selector.
 *
 * Mirrors web/src/components/layout/ThemeToggle.tsx. Mounted in the Header.
 * Persists via the theme store (localStorage), which writes
 * <html data-theme>. The trigger icon swaps instantly with the theme (no
 * rotate or scale animation: Boxy motion is mechanical).
 */
import { MonitorIcon, MoonIcon, SunIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useTheme } from "@/hooks/useTheme";
import type { Theme } from "@/lib/stores/theme-store";

const OPTIONS = [
  { value: "light", icon: SunIcon, labelKey: "theme.light" },
  { value: "dark", icon: MoonIcon, labelKey: "theme.dark" },
  { value: "system", icon: MonitorIcon, labelKey: "theme.system" },
] as const satisfies readonly { value: Theme; icon: unknown; labelKey: string }[];

export function ThemeToggle() {
  const { t } = useTranslation();
  const { theme, setTheme } = useTheme();

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="icon" aria-label={t("theme.label")}>
          <SunIcon className="h-5 w-5 dark:hidden" aria-hidden="true" />
          <MoonIcon className="hidden h-5 w-5 dark:block" aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>{t("theme.label")}</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={theme} onValueChange={(v) => setTheme(v as Theme)}>
          {OPTIONS.map(({ value, icon: Icon, labelKey }) => (
            <DropdownMenuRadioItem key={value} value={value} data-active={theme === value}>
              <Icon aria-hidden="true" />
              {t(labelKey)}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
