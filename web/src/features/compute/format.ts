/**
 * Number formatting for the instance runtime views. Byte units (KiB, MiB)
 * and time units are locale-independent symbols; the numbers themselves
 * go through Intl so Persian digits render in the fa locale.
 */
import type { components } from "@api-schema";

type Runtime = components["schemas"]["ComputeInstanceRuntime"];

const BYTE_UNITS = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"] as const;

/** formatBytes renders a byte count with binary units; "—" for unknown. */
export function formatBytes(bytes: number | undefined | null, locale?: string): string {
  if (bytes === undefined || bytes === null || bytes < 0 || !Number.isFinite(bytes)) return "—";
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < BYTE_UNITS.length - 1) {
    value /= 1024;
    unit++;
  }
  const digits = unit === 0 || value >= 100 ? 0 : 1;
  const n = new Intl.NumberFormat(locale, { maximumFractionDigits: digits }).format(value);
  return `${n} ${BYTE_UNITS[unit]}`;
}

/** formatNumber renders an integer with grouping; "—" for unknown. */
export function formatNumber(n: number | undefined | null, locale?: string): string {
  if (n === undefined || n === null || !Number.isFinite(n)) return "—";
  return new Intl.NumberFormat(locale).format(n);
}

/** formatCpuTime renders CPU nanoseconds as h/m/s. */
export function formatCpuTime(ns: number | undefined | null, locale?: string): string {
  if (ns === undefined || ns === null || ns < 0) return "—";
  const totalSeconds = ns / 1e9;
  const fmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 1 });
  if (totalSeconds < 60) return `${fmt.format(totalSeconds)} s`;
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = Math.floor(totalSeconds % 60);
  if (minutes < 60) return `${fmt.format(minutes)} m ${fmt.format(seconds)} s`;
  const hours = Math.floor(minutes / 60);
  return `${fmt.format(hours)} h ${fmt.format(minutes % 60)} m`;
}

/**
 * Where a config key or device comes from: set on the instance itself,
 * inherited from a profile, or daemon-managed (volatile.*).
 */
export type Origin = "instance" | "profile" | "system";

export function configOrigin(runtime: Runtime, key: string): Origin {
  if (key.startsWith("volatile.") || key.startsWith("image.")) return "system";
  return key in runtime.config ? "instance" : "profile";
}

export function deviceOrigin(runtime: Runtime, name: string): Origin {
  return name in runtime.devices ? "instance" : "profile";
}

/** devicesOfType returns expanded devices of one type, sorted by name. */
export function devicesOfType(runtime: Runtime, type: string): [string, Record<string, string>][] {
  return Object.entries(runtime.expandedDevices)
    .filter(([, props]) => props.type === type)
    .sort(([a], [b]) => a.localeCompare(b));
}
