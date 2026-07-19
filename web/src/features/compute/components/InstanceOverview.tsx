/**
 * Overview — first tab on the instance detail page.
 *
 * Shows the immutable attributes (id, tenant, image, fingerprint,
 * profiles) and the description. Lifecycle buttons live in the page
 * header so they're visible regardless of which tab is active.
 */
import type { components } from "@api-schema";
import { useTranslation } from "react-i18next";

import { Badge } from "@/components/ui/badge";
import { InstanceStatusBadge } from "./InstanceStatusBadge";

type Instance = components["schemas"]["ComputeInstance"];

interface Props {
  instance: Instance;
}

export function InstanceOverview({ instance }: Props) {
  const { t } = useTranslation();
  const profiles = instance.profiles ?? [];

  return (
    <dl className="grid gap-x-6 gap-y-4 sm:grid-cols-2">
      <Field label={t("compute.detail.id")}>
        <code className="font-mono text-xs">{instance.id}</code>
      </Field>
      <Field label={t("compute.status.other")}>
        <InstanceStatusBadge status={instance.status} />
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
      <Field label={t("compute.detail.description")}>
        <span className="text-sm">{instance.description ?? t("compute.detail.noDescription")}</span>
      </Field>
    </dl>
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
