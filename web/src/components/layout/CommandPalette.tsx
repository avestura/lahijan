/**
 * CommandPalette — the global ⌘K palette.
 *
 * Triggered by ⌘K / Ctrl+K (and via the Search button in the Header).
 * Lets the user jump to any nav area, instance, zone, or bucket by
 * typing.
 *
 * Implementation notes:
 *   - `cmdk`'s `<Command>` primitive is wrapped in a shadcn-style Dialog
 *     so the palette behaves like a modal: focus trap, Esc to dismiss,
 *     click-outside to close.
 *   - The list of destinations is fixed for MVP (the primary nav). Live
 *     instance/zone/bucket search is a Phase-7 enhancement; for now we
 *     filter against the localized nav labels so the palette is useful
 *     without an extra round-trip.
 */
import { Command } from "cmdk";
import {
  BoxesIcon,
  CloudIcon,
  DatabaseIcon,
  DollarSignIcon,
  LayoutDashboardIcon,
  PlugIcon,
  ScrollTextIcon,
  SearchIcon,
  SettingsIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "@tanstack/react-router";

import { Dialog, DialogContent } from "@/components/ui/dialog";

interface Destination {
  id: string;
  labelKey: string;
  to: string;
  icon: React.ComponentType<{ className?: string }>;
  keywords?: string;
}

const DESTINATIONS: Destination[] = [
  { id: "dashboard", labelKey: "nav.dashboard", to: "/dashboard", icon: LayoutDashboardIcon },
  {
    id: "compute",
    labelKey: "nav.compute",
    to: "/compute",
    icon: CloudIcon,
    keywords: "instances vm container",
  },
  {
    id: "dns",
    labelKey: "nav.dns",
    to: "/dns",
    icon: GlobeWrapper,
    keywords: "zones records",
  },
  {
    id: "storage",
    labelKey: "nav.storage",
    to: "/storage",
    icon: DatabaseIcon,
    keywords: "buckets s3 objects",
  },
  {
    id: "billing",
    labelKey: "nav.billing",
    to: "/billing",
    icon: DollarSignIcon,
    keywords: "ledger receipts",
  },
  { id: "plugins", labelKey: "nav.plugins", to: "/plugins", icon: PlugIcon },
  { id: "audit", labelKey: "nav.audit", to: "/audit", icon: ScrollTextIcon },
  { id: "settings", labelKey: "nav.settings", to: "/settings", icon: SettingsIcon },
  {
    id: "settings.security",
    labelKey: "nav.settings.security",
    to: "/settings/security",
    icon: SettingsIcon,
  },
  {
    id: "settings.tokens",
    labelKey: "nav.settings.tokens",
    to: "/settings/tokens",
    icon: SettingsIcon,
  },
  { id: "admin.jobs", labelKey: "nav.admin.jobs", to: "/admin/jobs", icon: BoxesIcon },
];

// Wrap lucide's GlobeIcon so we can swap it for a project-local icon later
// without churning every nav item. Mirrors the Sidebar's GlobeWrapper.
function GlobeWrapper({ className }: { className?: string }) {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="10" />
      <path d="M2 12h20" />
      <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z" />
    </svg>
  );
}

interface CommandPaletteProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function CommandPalette({ open, onOpenChange }: CommandPaletteProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();

  const go = async (to: string) => {
    onOpenChange(false);
    await navigate({ to });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="overflow-hidden p-0 shadow-2xl">
        <Command
          label={t("commandPalette.label")}
          className="flex flex-col overflow-hidden rounded-xl"
        >
          <div className="flex items-center border-b border-border px-3">
            <SearchIcon className="me-2 h-4 w-4 shrink-0 text-muted-foreground" />
            <Command.Input
              autoFocus
              placeholder={t("commandPalette.placeholder")}
              className="flex h-11 w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
            />
          </div>
          <Command.List className="max-h-[320px] overflow-y-auto p-1">
            <Command.Empty className="p-6 text-center text-sm text-muted-foreground">
              {t("commandPalette.empty")}
            </Command.Empty>
            <Command.Group heading={t("commandPalette.goto")}>
              {DESTINATIONS.map((d) => {
                const Icon = d.icon;
                return (
                  <Command.Item
                    key={d.id}
                    value={`${t(d.labelKey)} ${d.keywords ?? ""}`}
                    onSelect={() => go(d.to)}
                  >
                    <Icon className="me-2 h-4 w-4 text-muted-foreground" />
                    <span className="flex-1">{t(d.labelKey)}</span>
                  </Command.Item>
                );
              })}
            </Command.Group>
          </Command.List>
        </Command>
      </DialogContent>
    </Dialog>
  );
}
