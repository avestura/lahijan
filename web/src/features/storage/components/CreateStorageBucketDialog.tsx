/**
 * CreateStorageBucketDialog — "New bucket" modal.
 */
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
import { useCreateStorageBucket } from "../api";
import { createBucketSchema, type CreateBucketValues } from "../schemas";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  tenantId: string | null;
}

export function CreateStorageBucketDialog({ open, onOpenChange, tenantId }: Props) {
  const { t } = useTranslation();
  const create = useCreateStorageBucket(tenantId);

  const form = useForm<CreateBucketValues>({
    resolver: zodResolver(createBucketSchema),
    defaultValues: {
      slug: "",
      label: "",
      description: "",
      quotaBytes: 0,
      quotaObjects: 0,
    },
    mode: "onChange",
  });

  const onSubmit = form.handleSubmit(async (values) => {
    await create.mutateAsync(values);
    onOpenChange(false);
    form.reset({ slug: "", label: "", description: "", quotaBytes: 0, quotaObjects: 0 });
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("storage.create.title")}</DialogTitle>
          <DialogDescription>{t("storage.create.subtitle")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="bucket-slug">{t("storage.create.slug.label")}</Label>
            <Input
              id="bucket-slug"
              placeholder={t("storage.create.slug.placeholder")}
              aria-describedby="bucket-slug-hint"
              {...form.register("slug")}
            />
            <p id="bucket-slug-hint" className="text-xs text-muted-foreground">
              {t("storage.create.slug.hint")}
            </p>
            {form.formState.errors.slug && (
              <p className="text-xs text-destructive">{form.formState.errors.slug.message}</p>
            )}
          </div>
          <div className="space-y-2">
            <Label htmlFor="bucket-label">{t("storage.create.label.label")}</Label>
            <Input
              id="bucket-label"
              placeholder={t("storage.create.label.placeholder")}
              {...form.register("label")}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="bucket-desc">{t("storage.create.description.label")}</Label>
            <Textarea id="bucket-desc" rows={2} {...form.register("description")} />
          </div>
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label htmlFor="bucket-qb">{t("storage.create.quotaBytes.label")}</Label>
              <Input
                id="bucket-qb"
                type="number"
                min={0}
                {...form.register("quotaBytes", { valueAsNumber: true })}
              />
              <p className="text-xs text-muted-foreground">{t("storage.create.quotaBytes.hint")}</p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="bucket-qo">{t("storage.create.quotaObjects.label")}</Label>
              <Input
                id="bucket-qo"
                type="number"
                min={0}
                {...form.register("quotaObjects", { valueAsNumber: true })}
              />
              <p className="text-xs text-muted-foreground">
                {t("storage.create.quotaObjects.hint")}
              </p>
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? t("storage.create.submitting") : t("storage.create.submit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
