/**
 * Billing query + mutation hooks.
 *
 * User-side: balance / usage / ledger / receipts.
 * Admin-side: prices, top-up, refund, user ledger, user balance.
 *
 * Per WS-17: every admin mutation emits an audit event server-side.
 */
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { components } from "@api-schema";

import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/api/keys";
import { useToast } from "@/hooks/useToast";
import { useTranslation } from "react-i18next";
import type {
  GenerateReceiptValues,
  PriceUpsertValues,
  RefundValues,
  TopupValues,
} from "./schemas";

type BillingBalance = components["schemas"]["BillingBalance"];
type BillingUsageEvent = components["schemas"]["BillingUsageEvent"];
type BillingLedgerEntry = components["schemas"]["BillingLedgerEntry"];
type BillingReceipt = components["schemas"]["BillingReceipt"];
type BillingPrice = components["schemas"]["BillingPrice"];

// ---------------------------------------------------------------------------
// User
// ---------------------------------------------------------------------------

/** useMyBalance — GET /me/balance. */
export function useMyBalance() {
  return useQuery({
    queryKey: queryKeys.billing.balance(),
    staleTime: 30_000,
    queryFn: async (): Promise<BillingBalance> => {
      const { data, error, response } = await apiClient.GET("/api/v1/me/balance");
      if (error || !data) {
        throw new Error(`me.balance.get: ${response?.status ?? "network"}`);
      }
      return data;
    },
  });
}

/** useMyUsage — GET /me/usage with optional resourceType + window. */
export function useMyUsage(filters?: { resourceType?: string; from?: string; to?: string }) {
  return useQuery({
    queryKey: queryKeys.billing.usage(filters ?? {}),
    staleTime: 60_000,
    queryFn: async (): Promise<BillingUsageEvent[]> => {
      const { data, error, response } = await apiClient.GET("/api/v1/me/usage", {
        params: {
          query: {
            resourceType: filters?.resourceType,
            from: filters?.from,
            to: filters?.to,
          },
        },
      });
      if (error || !data) {
        throw new Error(`me.usage.list: ${response?.status ?? "network"}`);
      }
      return data.items;
    },
  });
}

/** useMyLedger — GET /me/ledger paginated. */
export function useMyLedger(offset = 0, limit = 25) {
  return useQuery({
    queryKey: queryKeys.billing.ledger({ offset, limit }),
    staleTime: 30_000,
    queryFn: async (): Promise<{ items: BillingLedgerEntry[]; total: number }> => {
      const { data, error, response } = await apiClient.GET("/api/v1/me/ledger", {
        params: { query: { offset, limit } },
      });
      if (error || !data) {
        throw new Error(`me.ledger.list: ${response?.status ?? "network"}`);
      }
      return { items: data.items, total: data.total };
    },
  });
}

/** useMyReceipts — GET /me/receipts paginated. */
export function useMyReceipts() {
  return useQuery({
    queryKey: queryKeys.billing.receipts(),
    staleTime: 60_000,
    queryFn: async (): Promise<BillingReceipt[]> => {
      const { data, error, response } = await apiClient.GET("/api/v1/me/receipts", {
        params: { query: { limit: 50 } },
      });
      if (error || !data) {
        throw new Error(`me.receipts.list: ${response?.status ?? "network"}`);
      }
      return data.items;
    },
  });
}

/** useGenerateReceipt — POST /me/receipts. */
export function useGenerateReceipt() {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async (values: GenerateReceiptValues): Promise<BillingReceipt> => {
      const { data, error, response } = await apiClient.POST("/api/v1/me/receipts", {
        body: values,
      });
      if (error || !data) {
        throw new Error(`me.receipts.generate: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.billing.receipts() });
      toast({ title: t("billing.mutations.receiptGenerateSuccess"), variant: "success" });
    },
  });
}

/**
 * downloadReceiptPDF — open the PDF URL in a new tab. We deliberately
 * use a direct window.open because the openapi-fetch client doesn't
 * have a typed binary fetch helper and we want a familiar browser UX.
 *
 * The URL is same-origin in production; the dev server proxies /api
 * to the backend so it works in dev too.
 */
export function downloadReceiptPDF(receiptId: string): void {
  window.open(`/api/v1/me/receipts/${receiptId}.pdf`, "_blank", "noopener,noreferrer");
}

// ---------------------------------------------------------------------------
// Admin: prices
// ---------------------------------------------------------------------------

/** useAdminPrices — GET /admin/billing/prices. */
export function useAdminPrices() {
  return useQuery({
    queryKey: queryKeys.billing.prices(),
    staleTime: 30_000,
    queryFn: async (): Promise<BillingPrice[]> => {
      const { data, error, response } = await apiClient.GET("/api/v1/admin/billing/prices", {
        params: { query: { limit: 100 } },
      });
      if (error || !data) {
        throw new Error(`admin.prices.list: ${response?.status ?? "network"}`);
      }
      return data.items;
    },
  });
}

/** useUpsertAdminPrice — POST /admin/billing/prices. */
export function useUpsertAdminPrice() {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async (values: PriceUpsertValues): Promise<BillingPrice> => {
      const body: components["schemas"]["BillingPriceUpsertRequest"] = {
        resourceType: values.resourceType,
        unit: values.unit,
        priceCents: values.priceCents,
        currency: values.currency,
      };
      if (values.effectiveFrom) body.effectiveFrom = values.effectiveFrom;
      const { data, error, response } = await apiClient.POST("/api/v1/admin/billing/prices", {
        body,
      });
      if (error || !data) {
        throw new Error(`admin.prices.upsert: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.billing.prices() });
      toast({ title: t("billing.mutations.priceSuccess"), variant: "success" });
    },
  });
}

// ---------------------------------------------------------------------------
// Admin: per-user
// ---------------------------------------------------------------------------

/** useAdminUserBalance — GET /admin/users/{userId}/balance. */
export function useAdminUserBalance(userId: string | undefined) {
  return useQuery({
    queryKey: userId ? queryKeys.billing.adminUserBalance(userId) : ["billing", "disabled"],
    enabled: !!userId,
    staleTime: 30_000,
    queryFn: async (): Promise<BillingBalance> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/users/{userId}/balance",
        { params: { path: { userId: userId! } } },
      );
      if (error || !data) {
        throw new Error(`admin.user.balance: ${response?.status ?? "network"}`);
      }
      return data;
    },
  });
}

/** useAdminUserLedger — GET /admin/users/{userId}/ledger paginated. */
export function useAdminUserLedger(userId: string | undefined, offset = 0, limit = 25) {
  return useQuery({
    queryKey: userId
      ? queryKeys.billing.adminUserLedger(userId, { offset, limit })
      : ["billing", "disabled"],
    enabled: !!userId,
    queryFn: async (): Promise<{ items: BillingLedgerEntry[]; total: number }> => {
      const { data, error, response } = await apiClient.GET("/api/v1/admin/users/{userId}/ledger", {
        params: { path: { userId: userId! }, query: { offset, limit } },
      });
      if (error || !data) {
        throw new Error(`admin.user.ledger: ${response?.status ?? "network"}`);
      }
      return { items: data.items, total: data.total };
    },
  });
}

/** useAdminTopupUser — POST /admin/users/{userId}/topup. */
export function useAdminTopupUser(userId: string | undefined) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async (values: TopupValues): Promise<BillingLedgerEntry> => {
      const body: components["schemas"]["BillingTopupRequest"] = {
        amountCents: values.amountCents,
        currency: values.currency,
      };
      if (values.reference) body.reference = values.reference;
      const { data, error, response } = await apiClient.POST("/api/v1/admin/users/{userId}/topup", {
        params: { path: { userId: userId! } },
        body,
      });
      if (error || !data) {
        throw new Error(`admin.user.topup: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      if (userId) {
        void qc.invalidateQueries({ queryKey: queryKeys.billing.adminUserBalance(userId) });
        void qc.invalidateQueries({ queryKey: queryKeys.billing.adminUserLedger(userId) });
      }
      toast({ title: t("billing.mutations.topupSuccess"), variant: "success" });
    },
  });
}

/** useAdminRefundUser — POST /admin/users/{userId}/refund. */
export function useAdminRefundUser(userId: string | undefined) {
  const qc = useQueryClient();
  const { toast } = useToast();
  const { t } = useTranslation();

  return useMutation({
    mutationFn: async (values: RefundValues): Promise<BillingLedgerEntry> => {
      const body: components["schemas"]["BillingRefundRequest"] = {
        amountCents: values.amountCents,
        currency: values.currency,
      };
      if (values.reference) body.reference = values.reference;
      if (values.chargeLedgerId) body.chargeLedgerId = values.chargeLedgerId;
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/users/{userId}/refund",
        { params: { path: { userId: userId! } }, body },
      );
      if (error || !data) {
        throw new Error(`admin.user.refund: ${response?.status ?? "network"}`);
      }
      return data;
    },
    onSuccess: () => {
      if (userId) {
        void qc.invalidateQueries({ queryKey: queryKeys.billing.adminUserBalance(userId) });
        void qc.invalidateQueries({ queryKey: queryKeys.billing.adminUserLedger(userId) });
      }
      toast({ title: t("billing.mutations.refundSuccess"), variant: "success" });
    },
  });
}
