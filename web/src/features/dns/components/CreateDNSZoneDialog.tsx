/**
 * CreateDNSZoneDialog — "New zone" modal.
 *
 * Uses react-hook-form + zod to drive the form; on success the modal
 * closes and the parent list invalidates automatically (TanStack
 * Query observer pattern).
 */
import { useState } from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useCreateDNSZone, useDNSTemplates } from "../api";
import { createZoneSchema, type CreateZoneValues } from "../schemas";

const ZONE_KINDS = ["Native", "Master", "Slave"] as const satisfies readonly string[];

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  tenantId: string | null;
}

export function CreateDNSZoneDialog({ open, onOpenChange, tenantId }: Props) {
  const { t } = useTranslation();
  const create = useCreateDNSZone(tenantId);
  const templates = useDNSTemplates();
  const [templateId, setTemplateId] = useState<string>("");

  const form = useForm<CreateZoneValues>({
    resolver: zodResolver(createZoneSchema),
    defaultValues: {
      name: "",
      description: "",
      kind: "Native",
      templateId: undefined,
    },
    mode: "onChange",
  });

  const onSubmit = form.handleSubmit(async (values) => {
    const finalValues: CreateZoneValues = {
      ...values,
      templateId: templateId || undefined,
    };
    await create.mutateAsync(finalValues);
    onOpenChange(false);
    form.reset({ name: "", description: "", kind: "Native", templateId: undefined });
    setTemplateId("");
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent data-testid="create-zone-dialog">
        <DialogHeader>
          <DialogTitle>{t("dns.create.title")}</DialogTitle>
          <DialogDescription>{t("dns.create.subtitle")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="dns-zone-name">{t("dns.create.name.label")}</Label>
            <Input
              id="dns-zone-name"
              placeholder={t("dns.create.name.placeholder")}
              aria-describedby="dns-zone-name-hint"
              data-testid="create-zone-name"
              {...form.register("name")}
            />
            <p id="dns-zone-name-hint" className="text-xs text-muted-foreground">
              {t("dns.create.name.hint")}
            </p>
            {form.formState.errors.name && (
              <p className="text-xs text-destructive">{form.formState.errors.name.message}</p>
            )}
          </div>
          <div className="space-y-2">
            <Label htmlFor="dns-zone-desc">{t("dns.create.description.label")}</Label>
            <Textarea
              id="dns-zone-desc"
              placeholder={t("dns.create.description.placeholder")}
              rows={2}
              {...form.register("description")}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="dns-zone-kind">{t("dns.create.kind.label")}</Label>
            <Select
              value={form.watch("kind")}
              onValueChange={(v) => form.setValue("kind", v as CreateZoneValues["kind"])}
            >
              <SelectTrigger id="dns-zone-kind">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ZONE_KINDS.map((k) => (
                  <SelectItem key={k} value={k}>
                    {t(`dns.kinds.${k}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-2">
            <Label htmlFor="dns-zone-template">{t("dns.create.template.label")}</Label>
            <Select value={templateId} onValueChange={setTemplateId}>
              <SelectTrigger id="dns-zone-template">
                <SelectValue placeholder={t("dns.create.template.none")} />
              </SelectTrigger>
              <SelectContent>
                {(templates.data ?? []).map((tpl) => (
                  <SelectItem key={tpl.id} value={tpl.id}>
                    {tpl.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">{t("dns.create.template.hint")}</p>
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => onOpenChange(false)}
              data-testid="create-zone-cancel"
            >
              {t("common.cancel")}
            </Button>
            <Button
              type="submit"
              disabled={create.isPending || !form.formState.isValid}
              data-testid="create-zone-submit"
            >
              {create.isPending ? t("dns.create.submitting") : t("dns.create.submit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
