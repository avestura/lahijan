/**
 * InstanceConfig — the Config tab on the instance detail page.
 *
 * Renders the `config` map (limits.cpu, limits.memory, ...). For now
 * this is read-only; PATCH /instances/{id} exists in the API so a
 * follow-up can make this editable.
 */
import type { components } from "@api-schema";
import { useTranslation } from "react-i18next";

import { Card, CardContent } from "@/components/ui/card";

type Instance = components["schemas"]["ComputeInstance"];

interface Props {
  instance: Instance;
}

export function InstanceConfig({ instance }: Props) {
  const { t } = useTranslation();
  const entries = instance.config ? Object.entries(instance.config) : [];
  return (
    <Card>
      <CardContent className="p-4">
        {entries.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("common.none")}</p>
        ) : (
          <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-2">
            {entries.map(([k, v]) => (
              <div key={k} className="space-y-1">
                <dt className="font-mono text-xs text-muted-foreground">{k}</dt>
                <dd className="font-mono text-sm">{v}</dd>
              </div>
            ))}
          </dl>
        )}
      </CardContent>
    </Card>
  );
}
