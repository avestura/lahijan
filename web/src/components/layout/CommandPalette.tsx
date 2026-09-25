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
  ArrowDownIcon,
  ArrowUpIcon,
  CloudIcon,
  CornerDownLeftIcon,
  DatabaseIcon,
  DollarSignIcon,
  GlobeIcon,
  LayoutDashboardIcon,
  ScrollTextIcon,
  SearchIcon,
  SettingsIcon,
  ShieldIcon,
  SparklesIcon,
} from "lucide-react";
import { useState } from "react";
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
    icon: GlobeIcon,
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

// Key names are symbols, not translatable copy. Arrow keys are drawn with
// icons: arrow characters fall back to a colour emoji font (icons.md).
const KEY_ESC = "esc";

/** Wraps the first case-insensitive match of `query` in an inverted block. */
function Highlight({ text, query }: { text: string; query: string }) {
  const q = query.trim();
  const at = q ? text.toLowerCase().indexOf(q.toLowerCase()) : -1;
  if (at < 0) return <>{text}</>;
  return (
    <>
      {text.slice(0, at)}
      <mark className="bg-surface-inverse px-px text-ink-inverse">
        {text.slice(at, at + q.length)}
      </mark>
      {text.slice(at + q.length)}
    </>
  );
}

function KeyHint({ keys, label }: { keys: React.ReactNode; label: string }) {
  return (
    <span className="flex items-center gap-1 font-mono text-2xs uppercase tracking-[0.08em] text-ink-subtle">
      <kbd className="flex h-4 items-center border border-line bg-surface px-1 font-mono text-2xs text-ink-muted [&>svg]:size-3">
        {keys}
      </kbd>
      {label}
    </span>
  );
}

interface CommandPaletteProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function CommandPalette({ open, onOpenChange }: CommandPaletteProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [query, setQuery] = useState("");

  const setOpen = (next: boolean) => {
    if (!next) setQuery("");
    onOpenChange(next);
  };

  const go = async (to: string) => {
    setOpen(false);
    await navigate({ to });
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      {/* 640px, 96px from the top (not centred), heavy border + hard shadow
          from DialogContent (components-forms.md command palette). */}
      <DialogContent className="top-24 max-w-[640px] translate-y-0 gap-0 overflow-hidden p-0">
        <Command label={t("commandPalette.label")} className="flex flex-col overflow-hidden">
          <div className="flex h-12 items-center gap-3 border-b border-line px-4">
            <SearchIcon className="h-4 w-4 shrink-0 text-ink-subtle" />
            <Command.Input
              autoFocus
              value={query}
              onValueChange={setQuery}
              placeholder={t("commandPalette.placeholder")}
              data-testid="command-palette-input"
              className="flex h-full w-full bg-transparent text-base outline-none placeholder:text-ink-subtle"
            />
          </div>
          <Command.List className="max-h-[min(360px,calc(100vh-240px))] overflow-y-auto">
            <Command.Empty className="px-4 py-8 font-mono text-label uppercase tracking-[0.08em] text-ink-subtle">
              {t("commandPalette.noResults", { query: query.trim() })}
            </Command.Empty>
            <Command.Group heading={t("commandPalette.goto")}>
              {DESTINATIONS.map((d) => {
                const Icon = d.icon;
                const label = t(d.labelKey);
                return (
                  <Command.Item
                    key={d.id}
                    value={`${label} ${d.keywords ?? ""}`}
                    onSelect={() => go(d.to)}
                    data-testid={`command-palette-item-${d.id}`}
                    className="flex items-center gap-3"
                  >
                    <Icon className="h-4 w-4 shrink-0" />
                    <span className="min-w-0 flex-1 truncate">
                      <Highlight text={label} query={query} />
                    </span>
                    {/* Real meta only: the route the row opens. */}
                    <span className="ms-auto font-mono text-label text-ink-subtle" dir="ltr">
                      {d.to}
                    </span>
                  </Command.Item>
                );
              })}
            </Command.Group>
          </Command.List>
          <div className="flex h-8 items-center gap-4 border-t border-line bg-surface-sunken px-4">
            <KeyHint
              keys={
                <>
                  <ArrowUpIcon aria-hidden="true" />
                  <ArrowDownIcon aria-hidden="true" />
                </>
              }
              label={t("commandPalette.hintNavigate")}
            />
            <KeyHint
              keys={<CornerDownLeftIcon aria-hidden="true" />}
              label={t("commandPalette.hintSelect")}
            />
            <KeyHint keys={KEY_ESC} label={t("commandPalette.hintClose")} />
          </div>
        </Command>
      </DialogContent>
    </Dialog>
  );
}
