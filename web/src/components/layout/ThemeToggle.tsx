/**
 * ThemeToggle — light / dark / system selector.
 *
 * Mounted in the Header. Persists via the theme store (localStorage).
 */
import { MonitorIcon, MoonIcon, SunIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useTheme } from "@/hooks/useTheme";
import type { Theme } from "@/lib/stores/theme-store";

const OPTIONS: { value: Theme; Icon: typeof SunIcon; labelKey: string }[] = [
  { value: "light", Icon: SunIcon, labelKey: "theme.light" },
  { value: "dark", Icon: MoonIcon, labelKey: "theme.dark" },
  { value: "system", Icon: MonitorIcon, labelKey: "theme.system" },
];

export function ThemeToggle() {
  const { t } = useTranslation();
  const { theme, setTheme } = useTheme();

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          aria-label={t("theme.label")}
          data-testid="theme-toggle"
        >
          {/* Swap, don't animate: motion is mechanical (no rotate/scale). */}
          <SunIcon className="h-4 w-4 dark:hidden" />
          <MoonIcon className="hidden h-4 w-4 dark:block" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {/* A picker: radio items reserve the mark slot; choosing closes it. */}
        <DropdownMenuRadioGroup value={theme} onValueChange={(v) => setTheme(v as Theme)}>
          {OPTIONS.map(({ value, Icon, labelKey }) => (
            <DropdownMenuRadioItem
              key={value}
              value={value}
              data-active={theme === value}
              data-testid={`theme-option-${value}`}
            >
              <Icon />
              {t(labelKey)}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
