/**
 * InstanceNetwork — the Network tab on the instance detail page.
 *
 * Surfaces the `devices` map's NIC entries (keys with type: nic). The
 * data model mirrors Incus': each device is a free-form string→string
 * map; we render the fields Incus defines (type, name, host_name,
 * ipv4.address, ipv6.address, ...).
 */
import type { components } from "@api-schema";
import { useTranslation } from "react-i18next";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/layout/EmptyState";
import { NetworkIcon } from "lucide-react";

type Instance = components["schemas"]["ComputeInstance"];

interface Props {
  instance: Instance;
}

interface NicDevice {
  name: string;
  type: string;
  raw: Record<string, string>;
}

function pickNics(devices: Instance["devices"]): NicDevice[] {
  if (!devices) return [];
  const out: NicDevice[] = [];
  for (const [name, props] of Object.entries(devices)) {
    if (props && props.type === "nic") {
      out.push({ name, type: props.type ?? "nic", raw: props });
    }
  }
  return out;
}

export function InstanceNetwork({ instance }: Props) {
  const { t } = useTranslation();
  const nics = pickNics(instance.devices);
  if (nics.length === 0) {
    return (
      <EmptyState icon={NetworkIcon} title={t("common.none")} description={t("common.none")} />
    );
  }
  return (
    <div className="space-y-3">
      {nics.map((nic) => (
        <Card key={nic.name}>
          <CardHeader>
            <CardTitle className="text-sm">{nic.name}</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-3">
              {Object.entries(nic.raw).map(([k, v]) => (
                <div key={k} className="space-y-1">
                  <dt className="text-xs uppercase tracking-wider text-muted-foreground">{k}</dt>
                  <dd className="font-mono text-xs">{v}</dd>
                </div>
              ))}
            </dl>
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
