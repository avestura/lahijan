/**
 * Header — top bar with locale + theme toggles and the user menu.
 *
 * The user menu shows the active tenant (when the user has multiple
 * memberships) and a Sign out entry that calls the logout mutation. When
 * the session is anonymous the bar shows a Sign in link instead.
 */
import { LogOutIcon, UserIcon } from "lucide-react";
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
import { LocaleToggle } from "./LocaleToggle";
import { ThemeToggle } from "./ThemeToggle";
import { useSignOut } from "@/features/auth/api";

export function Header() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const signOut = useSignOut();

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
    <header className="flex h-14 items-center justify-between gap-4 border-b border-border bg-background px-4">
      <div className="flex items-center gap-3" />
      <div className="flex items-center gap-1">
        <LocaleToggle />
        <ThemeToggle />
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" aria-label={t("auth.logout")}>
              <UserIcon className="h-5 w-5" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuLabel>{t("nav.settings")}</DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuItem asChild>
              <Link to="/settings">{t("nav.settings")}</Link>
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={onSignOut}
              className="text-destructive focus:text-destructive"
            >
              <LogOutIcon className="me-2 h-4 w-4" />
              {t("auth.logout")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </header>
  );
}
