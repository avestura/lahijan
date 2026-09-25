/**
 * Header — top bar with locale + theme toggles, tenant switcher, the
 * global Command+K trigger, and the user menu.
 *
 * The user menu shows the active tenant (when the user has multiple
 * memberships), a Settings entry, and a Sign out entry that calls the
 * logout mutation. When the session is anonymous the bar shows a Sign
 * in link instead.
 */
import {
  BellIcon,
  ChevronDownIcon,
  KeyRoundIcon,
  LogOutIcon,
  SearchIcon,
  ShieldIcon,
  UserIcon,
} from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "@tanstack/react-router";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuHead,
  DropdownMenuItem,
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
        className="flex h-topbar shrink-0 items-center justify-between gap-4 border-b border-line bg-surface px-4"
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
            <kbd className="ms-2 hidden border border-line bg-surface-sunken px-1 font-mono text-2xs uppercase text-ink-subtle sm:inline">
              {KBD_HINT}
            </kbd>
          </Button>
        </div>
        <div className="flex items-center gap-1">
          <TenantSwitcher />
          <Button variant="ghost" size="icon" aria-label={t("common.actions")} className="relative">
            {/* No unread badge: the count square is hidden at zero
                (components-overlays.md notification inbox). */}
            <BellIcon className="h-4 w-4" />
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
                <Avatar className="h-6 w-6">
                  <AvatarFallback>{initials}</AvatarFallback>
                </Avatar>
                <ChevronDownIcon className="h-4 w-4 text-ink-subtle" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-64">
              {/* Account head strip: who is signed in (components-overlays.md). */}
              <DropdownMenuHead data-testid="user-menu-head">
                <Avatar className="h-8 w-8">
                  <AvatarFallback>{initials}</AvatarFallback>
                </Avatar>
                <div className="min-w-0">
                  {user?.displayName && (
                    <p className="truncate text-sm font-medium text-ink">{user.displayName}</p>
                  )}
                  {user?.email && (
                    <p className="truncate font-mono text-label text-ink-subtle">{user.email}</p>
                  )}
                </div>
              </DropdownMenuHead>
              <DropdownMenuItem asChild>
                <Link to="/settings/profile">
                  <UserIcon />
                  {t("nav.settingsSub.profile")}
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link to="/settings/security">
                  <ShieldIcon />
                  {t("nav.settingsSub.security")}
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem asChild>
                <Link to="/settings/tokens">
                  <KeyRoundIcon />
                  {t("nav.settingsSub.tokens")}
                </Link>
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={onSignOut} data-testid="user-menu-logout">
                <LogOutIcon />
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
