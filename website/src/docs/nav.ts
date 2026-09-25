/**
 * Documentation navigation: the sidebar tree, the pager order and the list
 * of prerendered docs routes all come from this one structure.
 *
 * Every `slug` maps to `src/content/docs/<slug>.md`; the docs home is
 * `index.md` (slug ""). Titles here are the sidebar labels; each page's own
 * front matter carries its full title and description.
 *
 * The docs are written in English only; the site chrome around them is
 * translated.
 */
export interface DocsLink {
  slug: string;
  title: string;
}

export interface DocsGroup {
  title: string;
  items: DocsLink[];
}

export interface DocsSection {
  title: string;
  items: (DocsLink | DocsGroup)[];
}

export const isGroup = (item: DocsLink | DocsGroup): item is DocsGroup => "items" in item;

export const DOCS_NAV: DocsSection[] = [
  {
    title: "Get started",
    items: [
      { slug: "", title: "Overview" },
      { slug: "getting-started/introduction", title: "Introduction" },
      { slug: "getting-started/quickstart", title: "Quickstart" },
      { slug: "getting-started/installation", title: "Install on a server" },
      { slug: "getting-started/first-steps", title: "First steps" },
      { slug: "getting-started/concepts", title: "Core concepts" },
    ],
  },
  {
    title: "Guides",
    items: [
      {
        title: "Compute",
        items: [
          { slug: "compute/overview", title: "Overview" },
          { slug: "compute/instances", title: "Instances" },
          { slug: "compute/console", title: "Console and exec" },
          { slug: "compute/snapshots-and-backups", title: "Snapshots and backups" },
          { slug: "compute/images", title: "Images" },
          { slug: "compute/profiles", title: "Profiles" },
          { slug: "compute/networks", title: "Networks" },
          { slug: "compute/storage", title: "Storage volumes" },
          { slug: "compute/floating-ips", title: "Floating IPs" },
        ],
      },
      {
        title: "DNS",
        items: [
          { slug: "dns/overview", title: "Overview" },
          { slug: "dns/zones", title: "Zones" },
          { slug: "dns/records", title: "Records" },
          { slug: "dns/dnssec", title: "DNSSEC" },
          { slug: "dns/domains", title: "Domain registration" },
          { slug: "dns/templates", title: "Zone templates" },
        ],
      },
      {
        title: "Object storage",
        items: [
          { slug: "storage/overview", title: "Overview" },
          { slug: "storage/buckets", title: "Buckets" },
          { slug: "storage/credentials", title: "Access keys" },
          { slug: "storage/presigned-urls", title: "Pre-signed URLs" },
          { slug: "storage/quotas", title: "Quotas" },
          { slug: "storage/s3-clients", title: "Using S3 clients" },
        ],
      },
      {
        title: "Account",
        items: [
          { slug: "account/profile", title: "Profile" },
          { slug: "account/security", title: "Sign-in security" },
          { slug: "account/access-tokens", title: "Access tokens" },
          { slug: "account/identities", title: "Linked identities" },
          { slug: "account/sessions", title: "Sessions" },
        ],
      },
      {
        title: "Billing",
        items: [
          { slug: "billing/overview", title: "Balance and usage" },
          { slug: "billing/payments", title: "Payments and plans" },
        ],
      },
      { slug: "agent/overview", title: "Agent" },
      { slug: "audit/overview", title: "Audit log" },
    ],
  },
  {
    title: "Administration",
    items: [
      { slug: "admin/users-and-tenants", title: "Users and tenants" },
      { slug: "admin/roles-and-permissions", title: "Roles and permissions" },
      { slug: "admin/billing", title: "Billing administration" },
      { slug: "admin/plugins", title: "Plugin administration" },
      { slug: "admin/marketplace", title: "Plugin marketplace" },
      { slug: "admin/agent-policy", title: "Agent policy" },
      { slug: "admin/jobs", title: "Background jobs" },
      { slug: "admin/compute-cluster", title: "Compute cluster" },
    ],
  },
  {
    title: "Plugins",
    items: [
      { slug: "plugins/overview", title: "How plugins work" },
      { slug: "plugins/writing-plugins", title: "Writing a plugin" },
      { slug: "plugins/manifest", title: "Manifest reference" },
      { slug: "plugins/host-api", title: "Host API" },
      { slug: "plugins/packaging", title: "Packaging (.lahx)" },
    ],
  },
  {
    title: "Operations",
    items: [
      { slug: "operations/architecture", title: "Architecture" },
      { slug: "operations/deployment", title: "Deployment" },
      { slug: "operations/tls-and-domains", title: "TLS and domains" },
      { slug: "operations/environment", title: "Environment file" },
      { slug: "operations/authentication", title: "Sign-in providers and email" },
      { slug: "operations/upgrades", title: "Upgrades and migrations" },
      { slug: "operations/backups", title: "Backups" },
      { slug: "operations/observability", title: "Observability" },
      { slug: "operations/security", title: "Security hardening" },
      { slug: "operations/troubleshooting", title: "Troubleshooting" },
    ],
  },
  {
    title: "Reference",
    items: [
      { slug: "reference/configuration", title: "Configuration" },
      { slug: "reference/api", title: "REST API" },
      { slug: "reference/cli", title: "Command line" },
      { slug: "reference/permissions", title: "Permissions" },
      { slug: "reference/glossary", title: "Glossary" },
    ],
  },
];

/** Every page in reading order (the pager walks this). */
export const DOCS_PAGES: DocsLink[] = DOCS_NAV.flatMap((section) =>
  section.items.flatMap((item) => (isGroup(item) ? item.items : [item])),
);

/** Where a page sits: its section and (optional) group, for the breadcrumb. */
export function locateDoc(
  slug: string,
): { section: DocsSection; group?: DocsGroup; link: DocsLink } | null {
  for (const section of DOCS_NAV) {
    for (const item of section.items) {
      if (isGroup(item)) {
        const link = item.items.find((l) => l.slug === slug);
        if (link) return { section, group: item, link };
      } else if (item.slug === slug) {
        return { section, link: item };
      }
    }
  }
  return null;
}

/** Route path for a docs slug, relative to the router basename. */
export const docsPath = (slug: string) => (slug ? `/docs/${slug}` : "/docs");
