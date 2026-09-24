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
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useTheme } from "@/hooks/useTheme";
import type { Theme } from "@/lib/stores/theme-store";

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
        <DropdownMenuItem
          onClick={() => setTheme("light" as Theme)}
          data-active={theme === "light"}
          data-testid="theme-option-light"
        >
          <SunIcon className="me-2 h-4 w-4" />
          {t("theme.light")}
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => setTheme("dark" as Theme)}
          data-active={theme === "dark"}
          data-testid="theme-option-dark"
        >
          <MoonIcon className="me-2 h-4 w-4" />
          {t("theme.dark")}
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => setTheme("system" as Theme)}
          data-active={theme === "system"}
          data-testid="theme-option-system"
        >
          <MonitorIcon className="me-2 h-4 w-4" />
          {t("theme.system")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
