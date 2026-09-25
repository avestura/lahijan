/**
 * Sidebar — primary navigation.
 *
 * Renders the area links (Dashboard, Compute, DNS, Storage, Billing,
 * Plugins, Audit, Settings) plus a separate Administration section for
 * platform admins. Future feature workspaces will land their pages under
 * these nav entries; for now the entries are wired but route to a "not yet"
 * placeholder so we can prove the i18n + design pipeline end to end.
 */
import {
  BookOpenIcon,
  CloudIcon,
  DatabaseIcon,
  DollarSignIcon,
  GlobeIcon,
  KeyIcon,
  LayoutDashboardIcon,
  LinkIcon,
  MonitorIcon,
  PlugIcon,
  ScrollTextIcon,
  ShieldIcon,
  SparklesIcon,
  UserIcon,
  WalletIcon,
} from "lucide-react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";

import { cn } from "@/lib/utils";
import { useSessionStore } from "@/lib/stores/session-store";

const PLATFORM_ADMIN = "platform.admin";

interface NavItem {
  to: string;
  labelKey: string;
  icon: React.ComponentType<{ className?: string }>;
}

const PRIMARY_NAV: NavItem[] = [
  { to: "/dashboard", labelKey: "nav.dashboard", icon: LayoutDashboardIcon },
  { to: "/compute", labelKey: "nav.compute", icon: CloudIcon },
  { to: "/dns", labelKey: "nav.dns", icon: GlobeIcon },
  { to: "/storage", labelKey: "nav.storage", icon: DatabaseIcon },
  { to: "/billing", labelKey: "nav.billing", icon: DollarSignIcon },
  // WS-31: AI agent chat — natural-language control of Lahijan.
  { to: "/agent", labelKey: "nav.agent", icon: SparklesIcon },
  // `/plugins` (user-facing installed plugins view) is hidden until the
  // route exists — currently links to a 404. Admin-side plugin management
  // is at /admin/plugins; marketplace browsing is at /admin/marketplace.
  // { to: "/plugins", labelKey: "nav.plugins", icon: PlugIcon },
  { to: "/audit", labelKey: "nav.audit", icon: ScrollTextIcon },
  // NOTE: a top-level "Settings" entry is intentionally NOT present —
  // the SETTINGS_NAV section below already groups the settings pages,
  // and a duplicate entry here would make every sub-page highlight two
  // nav items at once.
];

const SETTINGS_NAV: NavItem[] = [
  { to: "/settings/profile", labelKey: "nav.settingsSub.profile", icon: UserIcon },
  { to: "/settings/security", labelKey: "nav.settingsSub.security", icon: ShieldIcon },
  { to: "/settings/tokens", labelKey: "nav.settingsSub.tokens", icon: KeyIcon },
  { to: "/settings/identities", labelKey: "nav.settingsSub.identities", icon: LinkIcon },
  { to: "/settings/sessions", labelKey: "nav.settingsSub.sessions", icon: MonitorIcon },
  // WS-31: BYOK model-provider config for the agent chat.
  { to: "/settings/agents", labelKey: "nav.settingsSub.agents", icon: SparklesIcon },
];

const ADMIN_NAV: NavItem[] = [
  { to: "/admin/billing", labelKey: "nav.admin.billing", icon: WalletIcon },
  // `/admin/jobs` has no SPA route; River's built-in UI is mounted at
  // `/admin/jobs/ui` when jobs are enabled (conf.jobs.enabled=true).
  // Hide the nav entry in deployments where jobs are disabled to avoid
  // a 404. Re-enable when the SPA grows its own jobs dashboard.
  // { to: "/admin/jobs", labelKey: "nav.admin.jobs", icon: BoxesIcon },
  { to: "/admin/plugins", labelKey: "nav.admin.plugins", icon: PlugIcon },
  { to: "/admin/marketplace", labelKey: "nav.admin.marketplace", icon: BookOpenIcon },
  // WS-31: tenant agent policy (allowlist / caps / denylist).
  { to: "/admin/agent", labelKey: "nav.admin.agent", icon: ShieldIcon },
];

export function Sidebar() {
  const { t } = useTranslation();
  const user = useSessionStore((s) => s.user);
  const roles = useSessionStore((s) => s.roles);
  const isAdmin = !!user && roles.includes(PLATFORM_ADMIN);

  return (
    <aside
      data-testid="sidebar"
      className="flex h-full w-sidebar shrink-0 flex-col border-e border-line bg-sidebar text-sidebar-foreground"
    >
      {/* Wordmark row aligns with the 48px top bar so the two rules meet. */}
      <div className="flex h-topbar shrink-0 items-center gap-2 border-b border-line px-4">
        <span
          aria-hidden="true"
          className="inline-flex h-6 w-6 items-center justify-center bg-surface-inverse font-mono text-xs font-semibold text-ink-inverse"
        >
          {t("app.name").charAt(0)}
        </span>
        <span className="font-display text-sm font-semibold tracking-[-0.02em] text-ink">
          {t("app.name")}
        </span>
      </div>
      <nav className="flex-1 overflow-y-auto pb-4">
        {PRIMARY_NAV.map((item) => (
          <NavLink key={item.to} item={item} />
        ))}

        <SectionLabel>{t("nav.settingsSub.label")}</SectionLabel>
        {SETTINGS_NAV.map((item) => (
          <NavLink key={item.to} item={item} />
        ))}

        {isAdmin && (
          <>
            <SectionLabel>{t("nav.admin.label")}</SectionLabel>
            {ADMIN_NAV.map((item) => (
              <NavLink key={item.to} item={item} />
            ))}
          </>
        )}
      </nav>
    </aside>
  );
}

function NavLink({ item }: { item: NavItem }) {
  const { t } = useTranslation();
  const Icon = item.icon;
  // Stable, i18n-safe selector for the e2e suite: nav-<path-segments>.
  // e.g. /settings/security -> nav-settings-security, /dashboard -> nav-dashboard.
  const testId = `nav-${item.to.split("/").filter(Boolean).join("-")}`;
  return (
    <Link
      to={item.to}
      data-testid={testId}
      // Full-bleed 32px rows; active = surface-active + ink + 500 weight +
      // a 2px accent bar on the inline-start edge (components-core.md).
      className={cn(
        "flex h-8 items-center gap-2 px-4 text-sm text-ink-muted",
        "transition-[background-color,color] duration-75 ease-linear hover:bg-surface-hover hover:text-ink",
      )}
      activeProps={{
        className:
          "!bg-sidebar-accent !text-sidebar-accent-fg font-medium shadow-[inset_2px_0_0_0_var(--bx-accent)] rtl:shadow-[inset_-2px_0_0_0_var(--bx-accent)]",
      }}
    >
      <Icon className="h-4 w-4" />
      {t(item.labelKey)}
    </Link>
  );
}

/** SectionLabel — mono label on a sunken strip ruled above and below. */
function SectionLabel({ children }: { children: ReactNode }) {
  return (
    <div className="mt-4 flex h-6 items-center border-y border-line-subtle bg-surface-sunken px-4 font-mono text-label uppercase tracking-[0.08em] text-ink-subtle">
      {children}
    </div>
  );
}
