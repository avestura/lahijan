/**
 * TenantSwitcher — picks the active tenant scope.
 *
 * Reads the user's memberships from the session store; if the user has
 * exactly one membership, the switcher is hidden (no choice to make).
 * Picking a tenant updates `currentTenantId`, which:
 *   - causes the refresh middleware to send the new `X-Tenant-Id` header
 *     on every outbound request; and
 *   - changes every tenant-scoped query key, so TanStack Query refetches
 *     the new tenant's data automatically.
 */
import { Building2Icon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useSessionStore } from "@/lib/stores/session-store";

export function TenantSwitcher() {
  const { t } = useTranslation();
  const user = useSessionStore((s) => s.user);
  const currentTenantId = useSessionStore((s) => s.currentTenantId);
  const setTenant = useSessionStore((s) => s.setTenant);

  const memberships = user?.memberships ?? [];
  if (memberships.length <= 1) {
    // Single-tenant users have nothing to switch between; keep the bar tidy.
    return null;
  }

  const current = memberships.find((m) => m.tenantId === currentTenantId) ?? memberships[0];
  if (!current) return null;

  const label = t("tenant.label");

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className="gap-2" aria-label={t("tenant.switch")}>
          <Building2Icon className="h-4 w-4 text-ink-subtle" />
          <span className="hidden text-sm font-medium sm:inline">
            {/* The membership has no tenant-name field today; show the role + id suffix for orientation. */}
            {label} · {current.tenantId.slice(0, 8)}
          </span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        <DropdownMenuLabel>{t("tenant.switch")}</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={current.tenantId} onValueChange={setTenant}>
          {memberships.map((m) => (
            <DropdownMenuRadioItem key={m.tenantId} value={m.tenantId} className="h-auto py-2">
              <span className="flex min-w-0 flex-col">
                <span className="truncate font-mono text-xs">{m.tenantId}</span>
                <span className="text-xs text-ink-subtle">{m.role}</span>
              </span>
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
