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
  BoxesIcon,
  CloudIcon,
  DatabaseIcon,
  DollarSignIcon,
  KeyIcon,
  LayoutDashboardIcon,
  LinkIcon,
  MonitorIcon,
  PlugIcon,
  ScrollTextIcon,
  SettingsIcon,
  ShieldIcon,
  UserIcon,
  WalletIcon,
} from "lucide-react";
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
  { to: "/dns", labelKey: "nav.dns", icon: GlobeWrapper },
  { to: "/storage", labelKey: "nav.storage", icon: DatabaseIcon },
  { to: "/billing", labelKey: "nav.billing", icon: DollarSignIcon },
  { to: "/plugins", labelKey: "nav.plugins", icon: PlugIcon },
  { to: "/audit", labelKey: "nav.audit", icon: ScrollTextIcon },
  { to: "/settings", labelKey: "nav.settings", icon: SettingsIcon },
];

const SETTINGS_NAV: NavItem[] = [
  { to: "/settings", labelKey: "nav.settingsSub.profile", icon: UserIcon },
  { to: "/settings/security", labelKey: "nav.settingsSub.security", icon: ShieldIcon },
  { to: "/settings/tokens", labelKey: "nav.settingsSub.tokens", icon: KeyIcon },
  { to: "/settings/identities", labelKey: "nav.settingsSub.identities", icon: LinkIcon },
  { to: "/settings/sessions", labelKey: "nav.settingsSub.sessions", icon: MonitorIcon },
];

const ADMIN_NAV: NavItem[] = [
  { to: "/admin/billing", labelKey: "nav.admin.billing", icon: WalletIcon },
  { to: "/admin/jobs", labelKey: "nav.admin.jobs", icon: BoxesIcon },
  { to: "/admin/plugins", labelKey: "nav.admin.plugins", icon: PlugIcon },
  { to: "/admin/marketplace", labelKey: "nav.admin.marketplace", icon: BookOpenIcon },
];

// Wrap lucide's GlobeIcon so we can swap it for a project-local icon later
// without churning every nav item. Keeps the import site tidy too.
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

export function Sidebar() {
  const { t } = useTranslation();
  const user = useSessionStore((s) => s.user);
  const roles = useSessionStore((s) => s.roles);
  const isAdmin = !!user && roles.includes(PLATFORM_ADMIN);

  return (
    <aside className="flex h-full w-60 shrink-0 flex-col border-e border-sidebar-border bg-sidebar text-sidebar-foreground">
      <div className="flex h-14 items-center gap-2 px-4 font-semibold">
        <span
          aria-hidden="true"
          className="inline-flex h-6 w-6 items-center justify-center rounded-md bg-primary text-primary-foreground"
        >
          {t("app.name").charAt(0)}
        </span>
        <span>{t("app.name")}</span>
      </div>
      <nav className="flex-1 space-y-1 overflow-y-auto p-2">
        {PRIMARY_NAV.map((item) => (
          <NavLink key={item.to} item={item} />
        ))}

        <div className="px-3 pb-1 pt-6 text-xs uppercase tracking-wider text-muted-foreground">
          {t("nav.settingsSub.label")}
        </div>
        {SETTINGS_NAV.map((item) => (
          <NavLink key={item.to} item={item} />
        ))}

        {isAdmin && (
          <>
            <div className="px-3 pb-1 pt-6 text-xs uppercase tracking-wider text-muted-foreground">
              {t("nav.admin.label")}
            </div>
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
  return (
    <Link
      to={item.to}
      className={cn(
        "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium",
        "text-sidebar-foreground/80 hover:bg-sidebar-accent hover:text-sidebar-accent-fg",
        "transition-colors",
      )}
      activeProps={{
        className: "bg-sidebar-accent text-sidebar-accent-fg",
      }}
    >
      <Icon className="h-4 w-4" />
      {t(item.labelKey)}
    </Link>
  );
}
