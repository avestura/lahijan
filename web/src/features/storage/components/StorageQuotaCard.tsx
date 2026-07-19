/**
 * StorageQuotaCard — form to push new quota dimensions to the backend.
 *
 * Per WS-16: a zero value on either dimension clears that ceiling.
 * The backend enforces both ceilings on every PUT.
 */
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import type { components } from "@api-schema";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { usePerm } from "@/lib/perm";
import { useSetStorageBucketQuota } from "../api";
import { quotaSchema, type QuotaValues } from "../schemas";

type StorageBucket = components["schemas"]["StorageBucket"];

interface Props {
  tenantId: string | null;
  bucket: StorageBucket;
}

export function StorageQuotaCard({ tenantId, bucket }: Props) {
  const { t } = useTranslation();
  const setQuota = useSetStorageBucketQuota(tenantId, bucket.id);
  const { hasPerm } = usePerm("s3.bucket.update");

  const form = useForm<QuotaValues>({
    resolver: zodResolver(quotaSchema),
    defaultValues: {
      quotaBytes: bucket.quotaBytes,
      quotaObjects: bucket.quotaObjects,
    },
    values: {
      quotaBytes: bucket.quotaBytes,
      quotaObjects: bucket.quotaObjects,
    },
  });

  const onSubmit = form.handleSubmit(async (values) => {
    await setQuota.mutateAsync(values);
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("storage.quota.title")}</CardTitle>
        <p className="text-xs text-muted-foreground">{t("storage.quota.subtitle")}</p>
      </CardHeader>
      <CardContent>
        <form onSubmit={onSubmit} className="space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="q-bytes">{t("storage.quota.quotaBytes.label")}</Label>
              <Input
                id="q-bytes"
                type="number"
                min={0}
                disabled={!hasPerm}
                {...form.register("quotaBytes", { valueAsNumber: true })}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="q-objects">{t("storage.quota.quotaObjects.label")}</Label>
              <Input
                id="q-objects"
                type="number"
                min={0}
                disabled={!hasPerm}
                {...form.register("quotaObjects", { valueAsNumber: true })}
              />
            </div>
          </div>
          <div className="flex justify-end">
            <Button type="submit" disabled={!hasPerm || setQuota.isPending}>
              {setQuota.isPending ? t("storage.quota.submitting") : t("storage.quota.submit")}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}
