/**
 * InstanceStorage — the Storage tab on the instance detail page.
 *
 * Surfaces the `devices` map's disk entries (root + any extra mounts).
 */
import type { components } from "@api-schema";
import { useTranslation } from "react-i18next";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { EmptyState } from "@/components/layout/EmptyState";
import { HardDriveIcon } from "lucide-react";

type Instance = components["schemas"]["ComputeInstance"];

interface Props {
  instance: Instance;
}

interface DiskDevice {
  name: string;
  type: string;
  raw: Record<string, string>;
}

function pickDisks(devices: Instance["devices"]): DiskDevice[] {
  if (!devices) return [];
  const out: DiskDevice[] = [];
  for (const [name, props] of Object.entries(devices)) {
    if (props && props.type === "disk") {
      out.push({ name, type: props.type ?? "disk", raw: props });
    }
  }
  return out;
}

export function InstanceStorage({ instance }: Props) {
  const { t } = useTranslation();
  const disks = pickDisks(instance.devices);
  if (disks.length === 0) {
    return (
      <EmptyState icon={HardDriveIcon} title={t("common.none")} description={t("common.none")} />
    );
  }
  return (
    <div className="space-y-3">
      {disks.map((disk) => (
        <Card key={disk.name}>
          <CardHeader>
            <CardTitle className="text-sm">{disk.name}</CardTitle>
          </CardHeader>
          <CardContent>
            <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-3">
              {Object.entries(disk.raw).map(([k, v]) => (
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
