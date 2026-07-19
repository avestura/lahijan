/**
 * Byte + count formatting helpers for the storage UI. Centralised so
 * en + fa locales use the same numeric style; Intl.NumberFormat
 * respects the active locale at call sites.
 */

/**
 * formatBytes — human-friendly byte count (KiB, MiB, GiB, TiB).
 *
 * Uses 1024 as the divisor (binary) so the values match the S3
 * convention. Returns the raw number as a string when bytes is 0
 * (most callers want "0 B" not "0.00 B").
 */
export function formatBytes(bytes: number | bigint | undefined): string {
  const n = typeof bytes === "bigint" ? Number(bytes) : (bytes ?? 0);
  if (!Number.isFinite(n) || n <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  const value = n / Math.pow(1024, i);
  const digits = i === 0 ? 0 : value < 10 ? 2 : value < 100 ? 1 : 0;
  return `${value.toFixed(digits)} ${units[i]}`;
}
