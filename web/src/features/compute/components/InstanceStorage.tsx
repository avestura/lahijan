/**
 * InstanceStorage — the Storage tab on the instance detail page.
 *
 * Lists the effective disk devices (root disk + extra volumes/mounts,
 * including those inherited from profiles) with their pool, mount path,
 * size limit and live usage. Usage is only reported for running instances
 * on storage pools that track it (e.g. not the plain directory driver).
 */
import type { components } from "@api-schema";
import { useTranslation } from "react-i18next";
import { HardDriveIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState } from "@/components/layout/EmptyState";
import { deviceOrigin, devicesOfType, formatBytes } from "../format";
import { RuntimeSection } from "./RuntimeSection";

type Runtime = components["schemas"]["ComputeInstanceRuntime"];

interface Props {
  runtime: Runtime;
}

const DISK_COLUMN_KEYS = new Set(["type", "pool", "path", "source", "size"]);

export function InstanceStorage({ runtime }: Props) {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const disks = devicesOfType(runtime, "disk");

  return (
    <RuntimeSection title={t("compute.storage.disks")} description={t("compute.storage.disksHint")}>
      {disks.length === 0 ? (
        <EmptyState icon={HardDriveIcon} title={t("compute.storage.noDisks")} />
      ) : (
        <Table data-testid="instance-disks">
          <TableHeader>
            <TableRow>
              <TableHead>{t("compute.devices.columns.name")}</TableHead>
              <TableHead>{t("compute.storage.columns.mount")}</TableHead>
              <TableHead>{t("compute.storage.columns.pool")}</TableHead>
              <TableHead className="text-end">{t("compute.storage.columns.size")}</TableHead>
              <TableHead className="text-end">{t("compute.storage.columns.usage")}</TableHead>
              <TableHead>{t("compute.devices.columns.settings")}</TableHead>
              <TableHead>{t("compute.devices.columns.source")}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {disks.map(([name, props]) => {
              const usage = runtime.disks[name];
              const used = usage?.usage;
              const extra = Object.entries(props).filter(([k]) => !DISK_COLUMN_KEYS.has(k));
              return (
                <TableRow key={name}>
                  <TableCell className="font-mono text-xs">{name}</TableCell>
                  <TableCell className="font-mono text-xs">
                    {props.path ?? "—"}
                    {props.source && (
                      <div className="text-muted-foreground">
                        {t("compute.storage.from", { source: props.source })}
                      </div>
                    )}
                  </TableCell>
                  <TableCell className="font-mono text-xs">{props.pool ?? "—"}</TableCell>
                  <TableCell className="text-end font-mono text-xs tabular-nums">
                    {props.size ?? t("compute.storage.unlimited")}
                  </TableCell>
                  <TableCell className="text-end font-mono text-xs tabular-nums">
                    {used !== undefined && used >= 0
                      ? formatBytes(used, locale)
                      : t("compute.storage.usageUnknown")}
                    {usage?.total !== undefined && usage.total > 0 && (
                      <div className="text-muted-foreground">
                        {t("compute.storage.ofTotal", { total: formatBytes(usage.total, locale) })}
                      </div>
                    )}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {extra.length === 0
                      ? "—"
                      : extra.map(([k, v]) => <div key={k}>{`${k}=${v}`}</div>)}
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">
                      {t(`compute.origin.${deviceOrigin(runtime, name)}`)}
                    </Badge>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      )}
    </RuntimeSection>
  );
}
