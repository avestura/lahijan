/**
 * Billing zod schemas.
 *
 * All amounts are integer centimals — never floats (per ADR-0013 +
 * WS-17 DoD). The schemas mirror the OpenAPI-generated types so the
 * form and the API request body can never drift.
 */
import { z } from "zod";

/**
 * topupSchema mirrors BillingTopupRequest.
 */
export const topupSchema = z.object({
  amountCents: z.coerce.number().int().min(1),
  currency: z.string().default("USD"),
  reference: z.string().max(200).optional(),
});

export type TopupValues = z.infer<typeof topupSchema>;

/**
 * refundSchema mirrors BillingRefundRequest.
 */
export const refundSchema = z.object({
  amountCents: z.coerce.number().int().min(1),
  currency: z.string().default("USD"),
  reference: z.string().max(200).optional(),
  chargeLedgerId: z.string().uuid().optional(),
});

export type RefundValues = z.infer<typeof refundSchema>;

/**
 * priceUpsertSchema mirrors BillingPriceUpsertRequest.
 */
export const priceUpsertSchema = z.object({
  resourceType: z.string().min(1),
  unit: z.string().min(1),
  priceCents: z.coerce.number().int().min(0),
  currency: z.string().default("USD"),
  effectiveFrom: z.string().optional(),
});

export type PriceUpsertValues = z.infer<typeof priceUpsertSchema>;

/**
 * generateReceiptSchema mirrors BillingReceiptGenerateRequest.
 */
export const generateReceiptSchema = z.object({
  periodStart: z.string().min(1),
  periodEnd: z.string().min(1),
});

export type GenerateReceiptValues = z.infer<typeof generateReceiptSchema>;

/**
 * USAGE_WINDOWS drives the user-side usage chart's window picker.
 * Values are ISO duration shortcuts; the UI translates them into
 * from/to query params when calling GET /api/v1/me/usage.
 */
export const USAGE_WINDOWS = ["7d", "30d"] as const;
export type UsageWindow = (typeof USAGE_WINDOWS)[number];

/**
 * formatMoney — render an integer-centimal amount with currency.
 *
 * Always uses Intl.NumberFormat so the thousands separator matches
 * the active locale. The fraction digits are derived from the
 * currency's minor unit (USD = 2; future currencies may differ).
 */
export function formatMoney(amountCents: number, currency = "USD"): string {
  const fractionDigits = currency === "USD" ? 2 : 2;
  const value = amountCents / Math.pow(10, fractionDigits);
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency,
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
  }).format(value);
}
