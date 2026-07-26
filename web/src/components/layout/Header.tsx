/**
 * Header — top bar with locale + theme toggles, tenant switcher, the
 * global Command+K trigger, and the user menu.
 *
 * The user menu shows the active tenant (when the user has multiple
 * memberships), a Settings entry, and a Sign out entry that calls the
 * logout mutation. When the session is anonymous the bar shows a Sign
 * in link instead.
 */
import { BellIcon, ChevronDownIcon, LogOutIcon, SearchIcon, UserIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "@tanstack/react-router";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import { LocaleToggle } from "./LocaleToggle";
import { ThemeToggle } from "./ThemeToggle";
import { CommandPalette } from "./CommandPalette";
import { useCommandPaletteShortcut } from "@/hooks/useCommandPaletteShortcut";
import { TenantSwitcher } from "./TenantSwitcher";
import { useSignOut } from "@/features/auth/api";
import { useSessionStore } from "@/lib/stores/session-store";

// The keyboard hint is a platform-aware symbol, not translatable copy.
// Computed at module load so the lint rule doesn't trip on a JSX literal.
const KBD_HINT =
  typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘K" : "Ctrl K";

export function Header() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const signOut = useSignOut();
  const user = useSessionStore((s) => s.user);
  const [paletteOpen, setPaletteOpen] = useState(false);
  useCommandPaletteShortcut(() => setPaletteOpen((o) => !o));

  const initials = useMemo(() => {
    const source = user?.displayName ?? user?.email ?? "";
    const head = source.trim().slice(0, 1);
    return head || "·";
  }, [user]);

  const onSignOut = () => {
    void signOut
      .mutateAsync()
      .then(async () => {
        await navigate({ to: "/login" });
      })
      .catch(() => {
        /* swallow — the store has already been reset */
      });
  };

  return (
    <>
      <header
        data-testid="app-header"
        className="flex h-14 items-center justify-between gap-4 border-b border-border bg-background px-4"
      >
        <div className="flex flex-1 items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            className="text-muted-foreground"
            onClick={() => setPaletteOpen(true)}
            aria-label={t("commandPalette.label")}
            data-testid="command-palette-trigger"
          >
            <SearchIcon className="h-4 w-4" />
            <span className="hidden md:inline">{t("commandPalette.placeholder")}</span>
            <kbd className="ms-2 hidden rounded border border-border bg-muted px-1 text-[10px] uppercase sm:inline">
              {KBD_HINT}
            </kbd>
          </Button>
        </div>
        <div className="flex items-center gap-1">
          <TenantSwitcher />
          <Button variant="ghost" size="icon" aria-label={t("common.actions")} className="relative">
            <BellIcon className="h-5 w-5" />
            <span className="absolute end-1.5 top-1.5 h-1.5 w-1.5 rounded-full bg-destructive" />
          </Button>
          <LocaleToggle />
          <ThemeToggle />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                className="gap-2"
                aria-label={t("auth.logout")}
                data-testid="user-menu-trigger"
              >
                <Avatar className="h-7 w-7">
                  <AvatarFallback>{initials}</AvatarFallback>
                </Avatar>
                <ChevronDownIcon className="h-4 w-4 text-muted-foreground" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
              <DropdownMenuLabel>{t("nav.settingsSub.label")}</DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem asChild>
                <Link to="/settings/profile">
                  <UserIcon className="me-2 h-4 w-4" />
                  {t("nav.settingsSub.profile")}
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link to="/settings/security">
                  <BellIcon className="me-2 h-4 w-4" />
                  {t("nav.settingsSub.security")}
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link to="/settings/tokens">
                  <SearchIcon className="me-2 h-4 w-4" />
                  {t("nav.settingsSub.tokens")}
                </Link>
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                onClick={onSignOut}
                className="text-destructive focus:text-destructive"
                data-testid="user-menu-logout"
              >
                <LogOutIcon className="me-2 h-4 w-4" />
                {t("auth.logout")}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </header>
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
    </>
  );
}
