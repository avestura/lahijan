/**
 * ImageStreamBrowser — searchable dialog over the linuxcontainers.org
 * image stream. Lets the user pick an image by OS / arch / variant and
 * returns its alias to the parent. See imageStream.ts for the feed shape.
 */
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { useLinuxContainerImages, type StreamImage } from "../imageStream";

const PAGE_SIZE = 50;

// Control values for the filter dropdowns. Held as consts so the JSX
// `value=` props read identifiers (not literals) — the i18n lint rule
// blocks bare string literals in user-facing props, and these are
// programmatic values rather than display copy.
const FILTER_ALL = "all";
const ARCH_FILTERS = ["amd64", "arm64", "armhf", "i386"] as const;

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onPick: (alias: string) => void;
}

export function ImageStreamBrowser({ open, onOpenChange, onPick }: Props) {
  const { t } = useTranslation();
  const query = useLinuxContainerImages();

  const [q, setQ] = useState("");
  const [osFilter, setOsFilter] = useState<string>(FILTER_ALL);
  const [archFilter, setArchFilter] = useState<string>(FILTER_ALL);
  const [visible, setVisible] = useState(PAGE_SIZE);

  const osOptions = useMemo(() => {
    const set = new Set<string>();
    for (const img of query.data ?? []) set.add(img.os);
    return Array.from(set).sort();
  }, [query.data]);

  const filtered = useMemo(() => {
    const lower = q.trim().toLowerCase();
    return (query.data ?? []).filter((img) => {
      if (osFilter !== FILTER_ALL && img.os !== osFilter) return false;
      if (archFilter !== FILTER_ALL && img.arch !== archFilter) return false;
      if (
        lower &&
        !img.alias.toLowerCase().includes(lower) &&
        !img.osTitle.toLowerCase().includes(lower) &&
        !img.releaseTitle.toLowerCase().includes(lower)
      ) {
        return false;
      }
      return true;
    });
  }, [query.data, q, osFilter, archFilter]);

  const page = filtered.slice(0, visible);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] max-w-3xl overflow-hidden">
        <DialogHeader>
          <DialogTitle>{t("compute.images.browser.title")}</DialogTitle>
          <DialogDescription>{t("compute.images.browser.subtitle")}</DialogDescription>
        </DialogHeader>

        <div className="flex flex-wrap items-center gap-2">
          <Input
            value={q}
            onChange={(e) => {
              setQ(e.target.value);
              setVisible(PAGE_SIZE);
            }}
            placeholder={t("compute.images.browser.search")}
            className="max-w-xs"
            aria-label={t("common.search")}
          />
          <Select
            value={osFilter}
            onValueChange={(v) => {
              setOsFilter(v);
              setVisible(PAGE_SIZE);
            }}
          >
            <SelectTrigger className="w-[140px]" aria-label={t("compute.images.browser.os")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={FILTER_ALL}>{t("compute.images.browser.allOS")}</SelectItem>
              {osOptions.map((os) => (
                <SelectItem key={os} value={os}>
                  {os}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            value={archFilter}
            onValueChange={(v) => {
              setArchFilter(v);
              setVisible(PAGE_SIZE);
            }}
          >
            <SelectTrigger className="w-[120px]" aria-label={t("compute.images.browser.arch")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={FILTER_ALL}>{t("compute.images.browser.allArch")}</SelectItem>
              {ARCH_FILTERS.map((a) => (
                <SelectItem key={a} value={a}>
                  {a}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <span className="ms-auto text-xs text-muted-foreground">
            {t("compute.images.browser.count", { shown: page.length, total: filtered.length })}
          </span>
        </div>

        <div className="max-h-[55vh] overflow-y-auto border border-border">
          {query.isLoading ? (
            <LoadingState rows={6} />
          ) : query.error ? (
            <div className="p-4">
              <ErrorState
                message={t("compute.images.browser.error")}
                retryLabel={t("common.retry")}
                onRetry={() => void query.refetch()}
              />
              <p className="mt-3 text-xs text-muted-foreground">
                {t("compute.images.browser.errorHint")}
              </p>
            </div>
          ) : page.length === 0 ? (
            <p className="p-6 text-center text-sm text-muted-foreground">
              {t("compute.images.browser.noResults")}
            </p>
          ) : (
            <ul className="divide-y divide-border">
              {page.map((img) => (
                <StreamRow key={`${img.alias}-${img.arch}`} img={img} onPick={onPick} />
              ))}
            </ul>
          )}
        </div>

        {visible < filtered.length && (
          <div className="flex justify-center">
            <Button variant="outline" size="sm" onClick={() => setVisible((v) => v + PAGE_SIZE)}>
              {t("compute.images.browser.loadMore")}
            </Button>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

function StreamRow({ img, onPick }: { img: StreamImage; onPick: (alias: string) => void }) {
  const { t } = useTranslation();
  return (
    <li className="flex items-center gap-3 px-3 py-2 hover:bg-surface-sunken">
      <div className="min-w-0 flex-1">
        <p className="truncate font-mono text-sm">{img.alias}</p>
        <p className="truncate text-xs text-muted-foreground">
          {img.osTitle} {img.releaseTitle} · {img.arch} · {img.variant}
        </p>
      </div>
      <Badge variant="outline" className="shrink-0 text-[10px]">
        {img.sizeBytes > 0 ? formatSize(img.sizeBytes) : "—"}
      </Badge>
      <Button
        size="sm"
        variant="ghost"
        onClick={() => onPick(img.alias)}
        aria-label={t("compute.images.browser.pick", { alias: img.alias })}
      >
        {t("compute.images.browser.use")}
      </Button>
    </li>
  );
}

function formatSize(bytes: number): string {
  if (!bytes) return "—";
  const units = ["B", "KiB", "MiB", "GiB"];
  const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)));
  return `${(bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}
