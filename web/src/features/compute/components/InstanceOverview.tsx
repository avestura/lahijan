/**
 * Overview — first tab on the instance detail page.
 *
 * Top: live figures from the runtime endpoint (IP addresses, CPU time,
 * memory, processes, architecture, created / last used). Below: the
 * immutable attributes (id, tenant, image, fingerprint, profiles) and the
 * description. Lifecycle buttons live in the page header so they're
 * visible regardless of which tab is active.
 */
import type { components } from "@api-schema";
import { useTranslation } from "react-i18next";

import { Badge } from "@/components/ui/badge";
import { InstanceStatusBadge } from "./InstanceStatusBadge";
import { formatBytes, formatCpuTime, formatNumber } from "../format";

type Instance = components["schemas"]["ComputeInstance"];
type Runtime = components["schemas"]["ComputeInstanceRuntime"];

interface Props {
  instance: Instance;
  /** Live view; undefined while loading or when the backend is unreachable. */
  runtime?: Runtime;
}

/** globalAddresses lists the guest's routable addresses (no loopback / link-local). */
function globalAddresses(runtime: Runtime | undefined): string[] {
  if (!runtime) return [];
  const out: string[] = [];
  for (const [name, nic] of Object.entries(runtime.networks)) {
    if (name === "lo") continue;
    for (const a of nic.addresses) {
      if (!a.scope || a.scope === "global") out.push(a.address);
    }
  }
  return out;
}

export function InstanceOverview({ instance, runtime }: Props) {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const profiles = runtime?.profiles ?? instance.profiles ?? [];
  const addresses = globalAddresses(runtime);
  const memory = runtime?.memory;
  const memTotal = memory?.total && memory.total > 0 ? memory.total : undefined;
  const memPct =
    memory?.usage !== undefined && memTotal ? Math.min(100, (memory.usage / memTotal) * 100) : null;
  const dateTime = (iso: string | undefined) =>
    iso ? new Date(iso).toLocaleString(locale) : t("common.none");

  return (
    <div className="space-y-6">
      <dl
        className="grid gap-px border border-border bg-border sm:grid-cols-2 lg:grid-cols-4"
        data-testid="instance-overview-metrics"
      >
        <Metric label={t("compute.overview.addresses")}>
          {addresses.length === 0 ? (
            <span className="text-sm text-muted-foreground">{t("common.none")}</span>
          ) : (
            <div className="font-mono text-sm">
              {addresses.map((a) => (
                <div key={a}>{a}</div>
              ))}
            </div>
          )}
        </Metric>
        <Metric label={t("compute.overview.memory")}>
          <div className="font-mono text-sm tabular-nums">
            {formatBytes(memory?.usage, locale)}
            {memTotal && (
              <span className="text-muted-foreground"> / {formatBytes(memTotal, locale)}</span>
            )}
          </div>
          {memPct !== null && (
            <div className="mt-2 h-1 w-full bg-muted" aria-hidden>
              <div className="h-1 bg-primary" style={{ width: `${memPct}%` }} />
            </div>
          )}
        </Metric>
        <Metric label={t("compute.overview.cpuTime")}>
          <span className="font-mono text-sm tabular-nums">
            {formatCpuTime(runtime?.cpuUsageNanoseconds, locale)}
          </span>
        </Metric>
        <Metric label={t("compute.overview.processes")}>
          <span className="font-mono text-sm tabular-nums">
            {formatNumber(runtime?.processes, locale)}
            {runtime?.pid ? (
              <span className="text-muted-foreground">
                {" "}
                {t("compute.overview.pid", { pid: formatNumber(runtime.pid, locale) })}
              </span>
            ) : null}
          </span>
        </Metric>
      </dl>

      <dl className="grid gap-x-6 gap-y-4 sm:grid-cols-2">
        <Field label={t("compute.detail.id")}>
          <code className="font-mono text-xs">{instance.id}</code>
        </Field>
        <Field label={t("compute.status.other")}>
          <InstanceStatusBadge status={runtime?.status ?? instance.status} />
        </Field>
        <Field label={t("compute.detail.tenant")}>
          <code className="font-mono text-xs">{instance.tenantId}</code>
        </Field>
        <Field label={t("compute.list.columns.type")}>
          <Badge variant="outline">{t(`compute.types.${instance.type ?? "container"}`)}</Badge>
        </Field>
        <Field label={t("compute.detail.imageAlias")}>
          <code className="font-mono text-xs">{instance.imageAlias}</code>
        </Field>
        <Field label={t("compute.detail.fingerprint")}>
          {instance.imageFingerprint ? (
            <code className="font-mono text-xs">{instance.imageFingerprint}</code>
          ) : (
            <span className="text-xs text-muted-foreground">{t("common.none")}</span>
          )}
        </Field>
        <Field label={t("compute.overview.architecture")}>
          <code className="font-mono text-xs">{runtime?.architecture ?? t("common.none")}</code>
        </Field>
        <Field label={t("compute.detail.profiles")}>
          {profiles.length > 0 ? (
            <div className="flex flex-wrap gap-1">
              {profiles.map((p) => (
                <Badge key={p} variant="secondary">
                  {p}
                </Badge>
              ))}
            </div>
          ) : (
            <span className="text-xs text-muted-foreground">{t("common.none")}</span>
          )}
        </Field>
        <Field label={t("compute.overview.created")}>
          <span className="text-sm">{dateTime(runtime?.createdAt ?? instance.createdAt)}</span>
        </Field>
        <Field label={t("compute.overview.lastUsed")}>
          <span className="text-sm">{dateTime(runtime?.lastUsedAt)}</span>
        </Field>
        <Field label={t("compute.detail.description")}>
          <span className="text-sm">
            {instance.description?.trim()
              ? instance.description
              : t("compute.detail.noDescription")}
          </span>
        </Field>
      </dl>
    </div>
  );
}

function Metric({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-2 bg-card p-4">
      <dt className="font-mono text-[11px] uppercase tracking-wider text-muted-foreground">
        {label}
      </dt>
      <dd>{children}</dd>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1">
      <dt className="text-xs uppercase tracking-wider text-muted-foreground">{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}
