/**
 * Dashboard-only formatting helpers. Kept separate from feature-specific
 * format utils (e.g. storage/format.ts) so the dashboard does not pull the
 * storage feature into its import graph when it only needs a currency
 * formatter.
 */

/**
 * formatCurrency — renders integer centimals as a localized currency string.
 *
 * balanceCents is signed (can be slightly negative during the billing grace
 * window); we let Intl handle the sign so RTL locales render it correctly.
 */
export function formatCurrency(balanceCents: number | undefined, currency: string): string {
  const cents = balanceCents ?? 0;
  const value = cents / 100;
  try {
    return new Intl.NumberFormat(undefined, {
      style: "currency",
      currency: currency || "USD",
      maximumFractionDigits: 2,
    }).format(value);
  } catch {
    return `${value.toFixed(2)} ${currency || ""}`.trim();
  }
}
