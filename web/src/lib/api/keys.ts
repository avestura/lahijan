/**
 * Typed query-key factory.
 *
 * Centralising query keys here keeps them stable across features, which
 * makes cache invalidation safe (mutate, then invalidate by prefix).
 *
 * Keys follow the TanStack Query convention: a tuple of [domain, scope,
 * ...params]. We never build keys by hand outside this file.
 */
export const queryKeys = {
  ping: () => ["platform", "ping"] as const,
  me: () => ["auth", "me"] as const,
  tenants: () => ["auth", "tenants"] as const,
  audit: (filters?: Record<string, unknown>) => ["audit", "list", filters ?? {}] as const,
  compute: {
    all: () => ["compute", "instances"] as const,
    list: () => ["compute", "instances", "list"] as const,
    detail: (id: string) => ["compute", "instances", "detail", id] as const,
  },
  dns: {
    zones: () => ["dns", "zones"] as const,
  },
  storage: {
    buckets: () => ["storage", "buckets"] as const,
  },
  billing: {
    balance: () => ["billing", "balance"] as const,
    ledger: () => ["billing", "ledger"] as const,
  },
} as const;

export type QueryKeyFactory = typeof queryKeys;
