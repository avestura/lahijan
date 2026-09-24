/**
 * InstanceNetwork — the Network tab on the instance detail page.
 *
 * Two views, both from the live runtime endpoint:
 *   - Interfaces: what the guest actually has right now (state, MAC, MTU,
 *     IPv4/IPv6 addresses, traffic counters). Running instances only.
 *   - NIC devices: the effective NIC configuration after profiles are
 *     applied, with where each device comes from (the instance itself or
 *     an inherited profile).
 */
import type { components } from "@api-schema";
import { useTranslation } from "react-i18next";
import { NetworkIcon } from "lucide-react";

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
import { deviceOrigin, devicesOfType, formatBytes, formatNumber } from "../format";
import { RuntimeSection } from "./RuntimeSection";

type Runtime = components["schemas"]["ComputeInstanceRuntime"];

interface Props {
  runtime: Runtime;
}

// Device keys surfaced as dedicated columns; the rest go to "Settings".
const NIC_COLUMN_KEYS = new Set(["type", "name", "network", "parent", "nictype"]);

export function InstanceNetwork({ runtime }: Props) {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const interfaces = Object.entries(runtime.networks).sort(([a], [b]) => a.localeCompare(b));
  const nics = devicesOfType(runtime, "nic");

  return (
    <div className="space-y-6">
      <RuntimeSection
        title={t("compute.network.interfaces")}
        description={t("compute.network.interfacesHint")}
      >
        {interfaces.length === 0 ? (
          <EmptyState
            icon={NetworkIcon}
            title={t("compute.network.noInterfaces")}
            description={t("compute.runtime.onlyWhileRunning")}
          />
        ) : (
          <Table data-testid="instance-interfaces">
            <TableHeader>
              <TableRow>
                <TableHead>{t("compute.network.columns.name")}</TableHead>
                <TableHead>{t("compute.network.columns.state")}</TableHead>
                <TableHead>{t("compute.network.columns.addresses")}</TableHead>
                <TableHead>{t("compute.network.columns.mac")}</TableHead>
                <TableHead className="text-end">{t("compute.network.columns.mtu")}</TableHead>
                <TableHead className="text-end">{t("compute.network.columns.rx")}</TableHead>
                <TableHead className="text-end">{t("compute.network.columns.tx")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {interfaces.map(([name, nic]) => (
                <TableRow key={name}>
                  <TableCell className="font-mono text-xs">
                    {name}
                    {nic.hostName && (
                      <div className="text-muted-foreground">
                        {t("compute.network.hostSide", { name: nic.hostName })}
                      </div>
                    )}
                  </TableCell>
                  <TableCell>
                    <Badge variant={nic.state === "up" ? "success" : "muted"}>
                      {nic.state ?? "—"}
                    </Badge>
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {nic.addresses.length === 0
                      ? "—"
                      : nic.addresses.map((a) => (
                          <div key={`${a.family}-${a.address}`}>
                            {a.address}
                            {a.netmask ? `/${a.netmask}` : ""}
                            {a.scope && a.scope !== "global" && (
                              <span className="text-muted-foreground"> ({a.scope})</span>
                            )}
                          </div>
                        ))}
                  </TableCell>
                  <TableCell className="font-mono text-xs">{nic.hwaddr ?? "—"}</TableCell>
                  <TableCell className="text-end font-mono text-xs tabular-nums">
                    {formatNumber(nic.mtu, locale)}
                  </TableCell>
                  <TableCell className="text-end font-mono text-xs tabular-nums">
                    {formatBytes(nic.bytesReceived, locale)}
                    <div className="text-muted-foreground">
                      {t("compute.network.packets", {
                        count: nic.packetsReceived ?? 0,
                        n: formatNumber(nic.packetsReceived, locale),
                      })}
                    </div>
                  </TableCell>
                  <TableCell className="text-end font-mono text-xs tabular-nums">
                    {formatBytes(nic.bytesSent, locale)}
                    <div className="text-muted-foreground">
                      {t("compute.network.packets", {
                        count: nic.packetsSent ?? 0,
                        n: formatNumber(nic.packetsSent, locale),
                      })}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </RuntimeSection>

      <RuntimeSection
        title={t("compute.network.devices")}
        description={t("compute.network.devicesHint")}
      >
        {nics.length === 0 ? (
          <EmptyState icon={NetworkIcon} title={t("compute.network.noDevices")} />
        ) : (
          <Table data-testid="instance-nic-devices">
            <TableHeader>
              <TableRow>
                <TableHead>{t("compute.devices.columns.name")}</TableHead>
                <TableHead>{t("compute.network.columns.attachedTo")}</TableHead>
                <TableHead>{t("compute.devices.columns.settings")}</TableHead>
                <TableHead>{t("compute.devices.columns.source")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {nics.map(([name, props]) => {
                const extra = Object.entries(props).filter(([k]) => !NIC_COLUMN_KEYS.has(k));
                return (
                  <TableRow key={name}>
                    <TableCell className="font-mono text-xs">{name}</TableCell>
                    <TableCell className="font-mono text-xs">
                      {props.network ?? props.parent ?? "—"}
                      {props.nictype && (
                        <span className="text-muted-foreground"> ({props.nictype})</span>
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
    </div>
  );
}
