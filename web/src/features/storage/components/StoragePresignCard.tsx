/**
 * StoragePresignCard — form to generate a pre-signed URL.
 *
 * The URL is shown in a follow-up panel with a copy button.
 */
import { useState } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useToast } from "@/hooks/useToast";
import { usePresignStorageObject } from "../api";
import { presignSchema, type PresignValues } from "../schemas";

const METHODS = ["GET", "PUT"] as const satisfies readonly string[];

interface Props {
  bucketId: string | undefined;
}

export function StoragePresignCard({ bucketId }: Props) {
  const { t } = useTranslation();
  const presign = usePresignStorageObject(bucketId);
  const { toast } = useToast();
  const [copied, setCopied] = useState(false);

  const form = useForm<PresignValues>({
    resolver: zodResolver(presignSchema),
    defaultValues: { method: "GET", key: "", expiresInSeconds: 3600 },
    mode: "onChange",
  });

  const onSubmit = form.handleSubmit(async (values) => {
    setCopied(false);
    await presign.mutateAsync(values);
  });

  const copyURL = async () => {
    if (!presign.data?.url) return;
    try {
      await navigator.clipboard.writeText(presign.data.url);
      setCopied(true);
      toast({ title: t("common.copied"), variant: "success" });
    } catch {
      /* ignore clipboard errors in sandbox */
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">{t("storage.presign.title")}</CardTitle>
        <p className="text-xs text-muted-foreground">{t("storage.presign.subtitle")}</p>
      </CardHeader>
      <CardContent className="space-y-4">
        <form onSubmit={onSubmit} className="space-y-3">
          <div className="grid grid-cols-[120px_1fr] items-center gap-2">
            <Label htmlFor="presign-method">{t("storage.presign.method.label")}</Label>
            <Select
              value={form.watch("method")}
              onValueChange={(v) => form.setValue("method", v as PresignValues["method"])}
            >
              <SelectTrigger id="presign-method">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {METHODS.map((m) => (
                  <SelectItem key={m} value={m}>
                    {m}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid grid-cols-[120px_1fr] items-center gap-2">
            <Label htmlFor="presign-key">{t("storage.presign.key.label")}</Label>
            <div className="space-y-1">
              <Input
                id="presign-key"
                placeholder={t("storage.presign.key.placeholder")}
                {...form.register("key")}
              />
              <p className="text-xs text-muted-foreground">{t("storage.presign.key.hint")}</p>
            </div>
          </div>
          <div className="grid grid-cols-[120px_1fr] items-center gap-2">
            <Label htmlFor="presign-exp">{t("storage.presign.expiresInSeconds.label")}</Label>
            <div className="space-y-1">
              <Input
                id="presign-exp"
                type="number"
                min={1}
                max={86400}
                {...form.register("expiresInSeconds", { valueAsNumber: true })}
              />
              <p className="text-xs text-muted-foreground">
                {t("storage.presign.expiresInSeconds.hint")}
              </p>
            </div>
          </div>
          <div className="flex justify-end">
            <Button type="submit" disabled={presign.isPending}>
              {presign.isPending ? t("storage.presign.submitting") : t("storage.presign.submit")}
            </Button>
          </div>
        </form>

        {presign.data && (
          <div className="space-y-2 rounded-md border border-border bg-muted/50 p-3">
            <div className="flex items-center justify-between">
              <Label>{t("storage.presign.result.title")}</Label>
              <span className="text-xs text-muted-foreground">
                {t("storage.presign.result.expiresAt")}:
                {" "}
                {new Date(presign.data.expiresAt).toLocaleString()}
              </span>
            </div>
            <code className="block break-all rounded bg-background p-2 font-mono text-xs">
              {presign.data.url}
            </code>
            <p className="text-xs text-muted-foreground">{t("storage.presign.result.warning")}</p>
            <div className="flex justify-end gap-2">
              <Button size="sm" variant="outline" onClick={copyURL}>
                {copied ? t("common.copied") : t("storage.presign.result.copy")}
              </Button>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
