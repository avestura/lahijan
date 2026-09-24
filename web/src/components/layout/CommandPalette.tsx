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
  CloudIcon,
  DatabaseIcon,
  DollarSignIcon,
  LayoutDashboardIcon,
  ScrollTextIcon,
  SearchIcon,
  SettingsIcon,
  ShieldIcon,
  SparklesIcon,
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
  // `/plugins` (user-facing) and `/admin/jobs` have no SPA routes yet;
  // commented out to avoid surfacing 404s in the command palette. See
  // Sidebar.tsx for the same omission + the rationale.
  // { id: "plugins", labelKey: "nav.plugins", to: "/plugins", icon: PlugIcon },
  { id: "audit", labelKey: "nav.audit", to: "/audit", icon: ScrollTextIcon },
  {
    id: "agent",
    labelKey: "nav.agent",
    to: "/agent",
    icon: SparklesIcon,
    keywords: "chat assistant ai",
  },
  {
    id: "settings.agents",
    labelKey: "nav.settingsSub.agents",
    to: "/settings/agents",
    icon: SparklesIcon,
    keywords: "agent provider api key model",
  },
  {
    id: "admin.agent",
    labelKey: "nav.admin.agent",
    to: "/admin/agent",
    icon: ShieldIcon,
    keywords: "agent policy limits denylist",
  },
  {
    id: "settings.profile",
    labelKey: "nav.settingsSub.profile",
    to: "/settings/profile",
    icon: SettingsIcon,
  },
  {
    id: "settings.security",
    labelKey: "nav.settingsSub.security",
    to: "/settings/security",
    icon: SettingsIcon,
  },
  {
    id: "settings.tokens",
    labelKey: "nav.settingsSub.tokens",
    to: "/settings/tokens",
    icon: SettingsIcon,
  },
  // { id: "admin.jobs", labelKey: "nav.admin.jobs", to: "/admin/jobs", icon: BoxesIcon },
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
      {/* 640px, 96px from the top (not centred), heavy border + hard shadow
          from DialogContent (components-forms.md command palette). */}
      <DialogContent className="top-24 max-w-[640px] translate-y-0 gap-0 overflow-hidden p-0">
        <Command label={t("commandPalette.label")} className="flex flex-col overflow-hidden">
          <div className="flex items-center border-b border-line px-4">
            <SearchIcon className="me-2 h-4 w-4 shrink-0 text-ink-subtle" />
            <Command.Input
              autoFocus
              placeholder={t("commandPalette.placeholder")}
              data-testid="command-palette-input"
              className="flex h-12 w-full bg-transparent text-base outline-none placeholder:text-ink-subtle"
            />
          </div>
          <Command.List className="max-h-80 overflow-y-auto">
            <Command.Empty className="p-6 font-mono text-label uppercase tracking-[0.08em] text-ink-subtle">
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
                    data-testid={`command-palette-item-${d.id}`}
                    className="flex items-center gap-2"
                  >
                    <Icon className="h-4 w-4 shrink-0 text-ink-subtle" />
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
