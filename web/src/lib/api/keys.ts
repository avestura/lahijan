/**
 * Typed query-key factory.
 *
 * Centralising query keys here keeps them stable across features, which
 * makes cache invalidation safe (mutate, then invalidate by prefix).
 *
 * Keys follow the TanStack Query convention: a tuple of [domain, scope,
 * ...params]. We never build keys by hand outside this file.
 *
 * Tenant-scoped queries embed the active tenant id in the key. When the
 * user switches tenants via the Header switcher, TanStack Query sees a
 * new key and refetches automatically — no manual invalidation needed.
 */
export const queryKeys = {
  ping: () => ["platform", "ping"] as const,
  me: () => ["auth", "me"] as const,
  tenants: () => ["auth", "tenants"] as const,
  audit: {
    list: (filters?: Record<string, unknown>) => ["audit", "list", filters ?? {}] as const,
    detail: (id: string) => ["audit", "detail", id] as const,
  },
  compute: {
    all: (tenantId: string) => ["compute", tenantId] as const,
    images: (tenantId: string) => ["compute", tenantId, "images"] as const,
    profiles: (tenantId: string) => ["compute", tenantId, "profiles"] as const,
    instances: (tenantId: string) => ["compute", tenantId, "instances"] as const,
    instance: (tenantId: string, id: string) =>
      ["compute", tenantId, "instances", "detail", id] as const,
    // WS-25: per-instance snapshot list + per-tenant snapshot policies /
    // backup targets / backups.
    snapshots: (tenantId: string, instanceId: string) =>
      ["compute", tenantId, "instances", "detail", instanceId, "snapshots"] as const,
    snapshotPolicies: (tenantId: string) =>
      ["compute", tenantId, "snapshot-policies"] as const,
    backupTargets: (tenantId: string) => ["compute", tenantId, "backup-targets"] as const,
    backups: (tenantId: string) => ["compute", tenantId, "backups"] as const,
  },
  dns: {
    zones: (tenantId: string) => ["dns", tenantId, "zones"] as const,
    zone: (tenantId: string, id: string) => ["dns", tenantId, "zones", "detail", id] as const,
    records: (tenantId: string, zoneId: string) =>
      ["dns", tenantId, "zones", zoneId, "records"] as const,
    templates: () => ["dns", "templates"] as const,
  },
  storage: {
    buckets: (tenantId: string) => ["storage", tenantId, "buckets"] as const,
    bucket: (tenantId: string, id: string) =>
      ["storage", tenantId, "buckets", "detail", id] as const,
    credentials: (tenantId: string, bucketId: string) =>
      ["storage", tenantId, "buckets", bucketId, "credentials"] as const,
    usage: (tenantId: string, bucketId: string) =>
      ["storage", tenantId, "buckets", bucketId, "usage"] as const,
  },
  billing: {
    balance: () => ["billing", "balance"] as const,
    usage: (filters?: Record<string, unknown>) => ["billing", "usage", filters ?? {}] as const,
    ledger: (filters?: Record<string, unknown>) => ["billing", "ledger", filters ?? {}] as const,
    receipts: () => ["billing", "receipts"] as const,
    prices: () => ["billing", "prices"] as const,
    adminUserBalance: (userId: string) => ["billing", "admin", "users", userId, "balance"] as const,
    adminUserLedger: (userId: string, filters?: Record<string, unknown>) =>
      ["billing", "admin", "users", userId, "ledger", filters ?? {}] as const,
  },
  plugins: {
    list: () => ["plugins", "list"] as const,
    detail: (id: string) => ["plugins", "detail", id] as const,
    marketplace: () => ["plugins", "marketplace"] as const,
    marketplaceEntry: (name: string) => ["plugins", "marketplace", name] as const,
  },
  my: {
    tokens: () => ["me", "tokens"] as const,
    identities: () => ["me", "identities"] as const,
    recovery: () => ["me", "mfa", "recovery"] as const,
  },
} as const;

export type QueryKeyFactory = typeof queryKeys;
