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
  audit: (filters?: Record<string, unknown>) => ["audit", "list", filters ?? {}] as const,
  compute: {
    all: (tenantId: string) => ["compute", tenantId] as const,
    images: (tenantId: string) => ["compute", tenantId, "images"] as const,
    profiles: (tenantId: string) => ["compute", tenantId, "profiles"] as const,
    instances: (tenantId: string) => ["compute", tenantId, "instances"] as const,
    instance: (tenantId: string, id: string) =>
      ["compute", tenantId, "instances", "detail", id] as const,
  },
  dns: {
    zones: (tenantId: string) => ["dns", tenantId, "zones"] as const,
  },
  storage: {
    buckets: (tenantId: string) => ["storage", tenantId, "buckets"] as const,
  },
  billing: {
    balance: () => ["billing", "balance"] as const,
    ledger: () => ["billing", "ledger"] as const,
  },
  my: {
    tokens: () => ["me", "tokens"] as const,
    identities: () => ["me", "identities"] as const,
    recovery: () => ["me", "mfa", "recovery"] as const,
  },
} as const;

export type QueryKeyFactory = typeof queryKeys;
