/**
 * linuxcontainers.org image stream client.
 *
 * The public simplestreams feed at
 *   https://images.linuxcontainers.org/streams/v1/images.json
 * is the same source Incus itself consults when you run
 * `incus launch ubuntu/24.04`. We fetch it client-side (the dashboard's
 * wizard surfaces a searchable browser so the picker is never empty even
 * before the operator seeds a featured catalog) and flatten the
 * `products:1.0` structure into a list of pickable images.
 *
 * The feed is ~1.5 MB; we cache the parsed flatten result for an hour via
 * TanStack Query so the cost is paid once per session.
 *
 * CORS: the endpoint does not advertise permissive CORS headers in every
 * deployment. The hook degrades gracefully (error state) and the wizard
 * also offers a manual alias entry, so a CORS-blocked environment is not
 * broken — it just loses the live browser.
 */
import { useQuery } from "@tanstack/react-query";

const STREAM_URL = "https://images.linuxcontainers.org/streams/v1/images.json";

/** A pickable image derived from one product in the stream. */
export interface StreamImage {
  /** Full alias, e.g. "ubuntu/24.04" or "debian/12/cloud". */
  alias: string;
  /** Short OS slug, e.g. "ubuntu", "debian" (lowercase). */
  os: string;
  /** Display name, e.g. "Ubuntu", "Debian". */
  osTitle: string;
  /** Release string, e.g. "24.04", "12". */
  release: string;
  /** Human release label, e.g. "24.04 LTS". */
  releaseTitle: string;
  /** Variant, e.g. "default", "cloud". */
  variant: string;
  /** Architecture, e.g. "amd64", "arm64". */
  arch: string;
  /** SHA-256 of the latest metadata tarball (the Incus fingerprint). */
  fingerprint: string;
  /** Bytes of the latest root/disk image. */
  sizeBytes: number;
}

interface StreamItem {
  ftype: string;
  sha256?: string;
  size?: number;
}
interface StreamVersion {
  items: Record<string, StreamItem>;
}
interface StreamProduct {
  aliases?: string;
  arch?: string;
  os?: string;
  release?: string;
  release_title?: string;
  variant?: string;
  versions?: Record<string, StreamVersion>;
}
interface StreamFeed {
  products: Record<string, StreamProduct>;
}

/** Architectures the browser offers as quick filters. */
export type StreamArch = "amd64" | "arm64" | "armhf" | "i386" | "ppc64el" | "s390x";

/**
 * pickLatestVersionKey — the highest build-timestamp key under a product's
 * `versions` map is the "current" build (simplestreams convention).
 */
function pickLatestVersionKey(versions: Record<string, StreamVersion> | undefined): string | null {
  if (!versions) return null;
  const keys = Object.keys(versions);
  if (keys.length === 0) return null;
  keys.sort();
  return keys[keys.length - 1] ?? null;
}

/**
 * flattenStream — collapse the nested products→versions→items structure
 * into one row per (product) with the latest version's fingerprint + size.
 *
 * We dedupe by alias+arch so a product that lists both `incus.tar.xz` and
 * `lxd.tar.xz` items only yields one row.
 */
export function flattenStream(feed: StreamFeed): StreamImage[] {
  const out: StreamImage[] = [];
  for (const product of Object.values(feed.products ?? {})) {
    const alias = product.aliases?.trim();
    const arch = product.arch ?? "";
    if (!alias || !arch) continue;
    const latestKey = pickLatestVersionKey(product.versions);
    const latest = latestKey ? product.versions?.[latestKey] : undefined;
    if (!latest) continue;
    // Prefer the Incus metadata tarball; fall back to any item with a sha.
    const meta =
      latest.items["incus.tar.xz"] ??
      latest.items["lxd.tar.xz"] ??
      Object.values(latest.items).find((i) => !!i.sha256);
    if (!meta?.sha256) continue;
    // Pick the data artifact size (rootfs/disk); 0 when only metadata present.
    const data =
      latest.items["disk.qcow2"] ??
      latest.items["root.squashfs"] ??
      latest.items["root.tar.xz"];
    out.push({
      alias,
      os: alias.split("/")[0] ?? product.os?.toLowerCase() ?? "",
      osTitle: product.os ?? alias.split("/")[0] ?? "",
      release: product.release ?? "",
      releaseTitle: product.release_title ?? product.release ?? "",
      variant: product.variant ?? "default",
      arch,
      fingerprint: meta.sha256,
      sizeBytes: data?.size ?? 0,
    });
  }
  // Stable, user-friendly ordering: by OS, then release (descending), then arch.
  out.sort((a, b) => {
    if (a.os !== b.os) return a.os.localeCompare(b.os);
    if (a.release !== b.release) return b.release.localeCompare(a.release);
    return a.arch.localeCompare(b.arch);
  });
  return out;
}

async function fetchStream(): Promise<StreamImage[]> {
  const res = await fetch(STREAM_URL, { headers: { Accept: "application/json" } });
  if (!res.ok) {
    throw new Error(`image stream: HTTP ${res.status}`);
  }
  const feed = (await res.json()) as StreamFeed;
  return flattenStream(feed);
}

/** Query key for the linuxcontainers stream cache. */
export const streamQueryKey = ["compute", "image-stream", "linuxcontainers"] as const;

/**
 * useLinuxContainerImages — TanStack Query wrapper around the stream fetch.
 * Cached for an hour; the feed changes daily so a long staleTime is safe.
 */
export function useLinuxContainerImages() {
  return useQuery({
    queryKey: streamQueryKey,
    queryFn: fetchStream,
    staleTime: 60 * 60 * 1000,
    gcTime: 24 * 60 * 60 * 1000,
    retry: 1,
  });
}
