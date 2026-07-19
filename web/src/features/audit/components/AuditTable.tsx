/**
 * AuditTable — paginated, filterable audit log.
 *
 * Each row is clickable; the detail dialog (AuditEventDialog) shows
 * the full metadata + outcome trail.
 *
 * Export buttons issue CSV + JSON downloads via direct anchor click
 * (the cookie auth is sent automatically).
 */
import * as React from "react";
import { useTranslation } from "react-i18next";
import { DownloadIcon, MoreHorizontalIcon, ScrollTextIcon } from "lucide-react";
import type { components } from "@api-schema";

import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/layout/EmptyState";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { usePerm } from "@/lib/perm";
import { buildAuditExportURL, useAuditEvents, type AuditFilters } from "../api";

type AuditEvent = components["schemas"]["AuditEvent"];

const PAGE_SIZE = 50;

const STATUS_OPTIONS = ["all", "success", "failure", "pending"] as const;
const ACTOR_TYPES = ["all", "user", "system", "plugin"] as const;

interface Props {
  filters: AuditFilters;
  setFilters: (f: AuditFilters) => void;
}

export function AuditTable({ filters, setFilters }: Props) {
  const { t } = useTranslation();
  const [offset, setOffset] = React.useState(0);
  const query = useAuditEvents(filters, offset, PAGE_SIZE);
  const { hasPerm: canExport } = usePerm("audit.export");
  const [detail, setDetail] = React.useState<AuditEvent | null>(null);

  const resetAndSet = (patch: Partial<AuditFilters>) => {
    setOffset(0);
    setFilters({ ...filters, ...patch });
  };

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-end gap-2">
        <div className="flex-1 space-y-1">
          <Label htmlFor="audit-action">{t("audit.filters.actionFilter")}</Label>
          <Input
            id="audit-action"
            value={filters.action ?? ""}
            onChange={(e) => resetAndSet({ action: e.target.value || undefined })}
          />
        </div>
        <div className="w-[160px] space-y-1">
          <Label htmlFor="audit-status">{t("audit.filters.status.label")}</Label>
          <Select
            value={filters.status ?? "all"}
            onValueChange={(v) =>
              resetAndSet({ status: v === "all" ? undefined : (v as AuditFilters["status"]) })
            }
          >
            <SelectTrigger id="audit-status">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {STATUS_OPTIONS.map((s) => (
                <SelectItem key={s} value={s}>
                  {s === "all" ? t("audit.filters.status.all") : t(`audit.filters.status.${s}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="w-[160px] space-y-1">
          <Label htmlFor="audit-actor">{t("audit.filters.actorType.label")}</Label>
          <Select
            value={filters.actorType ?? "all"}
            onValueChange={(v) =>
              resetAndSet({
                actorType: v === "all" ? undefined : (v as AuditFilters["actorType"]),
              })
            }
          >
            <SelectTrigger id="audit-actor">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {ACTOR_TYPES.map((a) => (
                <SelectItem key={a} value={a}>
                  {a === "all"
                    ? t("audit.filters.actorType.all")
                    : t(`audit.filters.actorType.${a}`)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="w-[200px] space-y-1">
          <Label htmlFor="audit-resource">{t("audit.filters.resourceType")}</Label>
          <Input
            id="audit-resource"
            value={filters.resourceType ?? ""}
            onChange={(e) => resetAndSet({ resourceType: e.target.value || undefined })}
          />
        </div>
        {canExport && (
          <div className="ms-auto">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm">
                  <DownloadIcon className="h-4 w-4" />
                  {t("audit.actions.export")}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem asChild>
                  <a
                    href={buildAuditExportURL("csv", filters)}
                    download={`${t("audit.export.filenamePrefix")}.csv`}
                  >
                    {t("audit.actions.exportCSV")}
                  </a>
                </DropdownMenuItem>
                <DropdownMenuItem asChild>
                  <a
                    href={buildAuditExportURL("json", filters)}
                    download={`${t("audit.export.filenamePrefix")}.json`}
                  >
                    {t("audit.actions.exportJSON")}
                  </a>
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        )}
      </div>

      {query.isLoading ? (
        <LoadingState rows={5} />
      ) : query.error ? (
        <ErrorState
          message={t("common.error")}
          retryLabel={t("common.retry")}
          onRetry={() => void query.refetch()}
        />
      ) : (query.data?.items ?? []).length === 0 ? (
        <EmptyState
          icon={ScrollTextIcon}
          title={t("audit.list.empty.title")}
          description={t("audit.list.empty.body")}
        />
      ) : (
        <>
          <div className="rounded-md border border-border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("audit.columns.action")}</TableHead>
                  <TableHead>{t("audit.columns.actor")}</TableHead>
                  <TableHead>{t("audit.columns.resource")}</TableHead>
                  <TableHead>{t("audit.columns.status")}</TableHead>
                  <TableHead>{t("audit.columns.created")}</TableHead>
                  <TableHead className="text-end">{t("audit.actions.viewDetail")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(query.data?.items ?? []).map((ev) => (
                  <TableRow key={ev.id}>
                    <TableCell>
                      <code className="font-mono text-xs">{ev.action}</code>
                    </TableCell>
                    <TableCell className="text-xs">
                      <div>{ev.actorType}</div>
                      {ev.actorUserId && (
                        <div className="font-mono text-[10px] text-muted-foreground">
                          {ev.actorUserId.slice(0, 8)}
                        </div>
                      )}
                    </TableCell>
                    <TableCell className="text-xs">
                      <div>{ev.resourceType}</div>
                      {ev.resourceId && (
                        <div className="font-mono text-[10px] text-muted-foreground">
                          {ev.resourceId.slice(0, 8)}
                        </div>
                      )}
                    </TableCell>
                    <TableCell>
                      <Badge variant={statusVariant(ev.status)}>
                        {t(`audit.filters.status.${ev.status}`)}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {new Date(ev.createdAt).toLocaleString()}
                    </TableCell>
                    <TableCell className="text-end">
                      <Button
                        size="icon"
                        variant="ghost"
                        aria-label={t("audit.actions.viewDetail")}
                        onClick={() => setDetail(ev)}
                      >
                        <MoreHorizontalIcon className="h-4 w-4" />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <div className="flex items-center justify-between text-xs text-muted-foreground">
            <span>
              {offset + 1}–{Math.min(offset + PAGE_SIZE, query.data?.total ?? 0)} /{" "}
              {query.data?.total ?? 0}
            </span>
            <div className="flex gap-2">
              <Button
                size="sm"
                variant="outline"
                disabled={offset === 0}
                onClick={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
              >
                {t("common.previous")}
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={offset + PAGE_SIZE >= (query.data?.total ?? 0)}
                onClick={() => setOffset((o) => o + PAGE_SIZE)}
              >
                {t("common.next")}
              </Button>
            </div>
          </div>
        </>
      )}

      <AuditEventDialog event={detail} onClose={() => setDetail(null)} />
    </div>
  );
}

function statusVariant(s: AuditEvent["status"]): "default" | "secondary" | "destructive" {
  if (s === "success") return "default";
  if (s === "failure") return "destructive";
  return "secondary";
}

/**
 * AuditEventDialog — modal showing the event's full metadata + the
 * outcome trail. The metadata blob is rendered as pretty JSON.
 */
function AuditEventDialog({ event, onClose }: { event: AuditEvent | null; onClose: () => void }) {
  const { t } = useTranslation();
  if (!event) return null;

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="audit-detail-title"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      onClick={onClose}
    >
      <div
        className="max-h-[80vh] w-full max-w-2xl overflow-y-auto rounded-md border border-border bg-background p-6 shadow-lg"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 id="audit-detail-title" className="mb-3 text-lg font-semibold">
          {t("audit.detail.title")}
        </h2>
        <dl className="space-y-2 text-sm">
          <DetailRow label={t("audit.detail.id")} value={event.id} />
          {event.tenantId && <DetailRow label={t("audit.detail.tenant")} value={event.tenantId} />}
          {event.actorType && (
            <DetailRow label={t("audit.detail.actorType")} value={event.actorType} />
          )}
          {event.actorUserId && (
            <DetailRow label={t("audit.detail.actorUser")} value={event.actorUserId} />
          )}
          <DetailRow label={t("audit.detail.action")} value={event.action} />
          <DetailRow label={t("audit.detail.resourceType")} value={event.resourceType} />
          {event.resourceId && (
            <DetailRow label={t("audit.detail.resourceId")} value={event.resourceId} />
          )}
          <DetailRow label={t("audit.detail.status")} value={event.status} />
          {event.requestId && (
            <DetailRow label={t("audit.detail.requestId")} value={event.requestId} />
          )}
          <DetailRow
            label={t("audit.detail.createdAt")}
            value={new Date(event.createdAt).toLocaleString()}
          />
        </dl>

        {event.metadata && Object.keys(event.metadata).length > 0 && (
          <div className="mt-4 space-y-1">
            <h3 className="text-sm font-medium">{t("audit.detail.metadata")}</h3>
            <pre className="overflow-x-auto rounded-md border border-border bg-muted/50 p-3 font-mono text-xs">
              {JSON.stringify(event.metadata, null, 2)}
            </pre>
          </div>
        )}

        {event.outcomes && event.outcomes.length > 0 && (
          <div className="mt-4 space-y-1">
            <h3 className="text-sm font-medium">{t("audit.detail.outcomes")}</h3>
            <ul className="space-y-1 text-xs">
              {event.outcomes.map((o) => (
                <li
                  key={o.id}
                  className="flex items-center justify-between gap-2 rounded-md border border-border p-2"
                >
                  <Badge variant={statusVariant(o.status)}>
                    {t(`audit.filters.status.${o.status}`)}
                  </Badge>
                  <span className="text-muted-foreground">
                    {new Date(o.createdAt).toLocaleString()}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        )}

        <div className="mt-6 flex justify-end">
          <Button variant="outline" onClick={onClose}>
            {t("common.close")}
          </Button>
        </div>
      </div>
    </div>
  );
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid grid-cols-[140px_1fr] items-center gap-2">
      <dt className="text-xs uppercase tracking-wider text-muted-foreground">{label}</dt>
      <dd className="break-all font-mono text-xs">{value}</dd>
    </div>
  );
}
