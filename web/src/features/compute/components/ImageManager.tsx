/**
 * ImageManager — the tenant's compute image catalog.
 *
 * Surfaces:
 *   - The catalog (featured + custom) as a table with delete on custom rows.
 *   - An "Add image" dialog (alias + fingerprint + type + arch + description).
 *   - A "Browse remote images" button that opens the linuxcontainers stream
 *     browser and pre-fills alias + fingerprint into the add dialog.
 *
 * State model: a single browse dialog + a single add dialog, plus a
 * `prefill` slot the browse dialog writes into when the user picks a row.
 * The add dialog consumes `prefill` via a `key` so react-hook-form re-mounts
 * with the right defaults (avoids fighting `useForm`'s one-shot defaults).
 */
import * as React from "react";
import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import { ImageIcon, PlusIcon, SearchIcon, Trash2Icon } from "lucide-react";
import type { components } from "@api-schema";

import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
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
import { EmptyState } from "@/components/layout/EmptyState";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { FeatureDisabledState } from "@/components/layout/FeatureDisabledState";
import { usePerm } from "@/lib/perm";
import { useTenant } from "@/hooks/useTenant";
import { isFeatureDisabledError } from "@/lib/api-errors";
import { useComputeImages, useDeleteComputeImage, useUploadComputeImage } from "../api";
import { ImageStreamBrowser } from "./ImageStreamBrowser";

type ComputeImage = components["schemas"]["ComputeImage"];

// Instance type codes mirror the OpenAPI enum; held in a const so JSX
// value props can read them as identifiers (the i18n lint rule blocks
// bare string literals in user-facing props, even when they are control
// values rather than display copy).
const INSTANCE_TYPES = ["container", "virtual-machine"] as const;

const addSchema = z.object({
  alias: z
    .string()
    .min(1)
    .max(63)
    .regex(/^[a-z0-9][a-z0-9/._-]*$/i, "Alias must be alphanumeric with / . - _ only."),
  fingerprint: z.string().min(1),
  type: z.enum(["container", "virtual-machine"]).default("container"),
  architecture: z.string().optional(),
  description: z.string().optional(),
});
type AddValues = z.infer<typeof addSchema>;

export function ImageManager() {
  const { t } = useTranslation();
  const tenant = useTenant();
  const tenantId = tenant.currentTenantId;
  const query = useComputeImages(tenantId);
  const destroy = useDeleteComputeImage(tenantId);

  const { hasPerm: canCreate } = usePerm("compute.image.create");
  const { hasPerm: canDelete } = usePerm("compute.image.delete");

  const [addOpen, setAddOpen] = React.useState(false);
  const [browseOpen, setBrowseOpen] = React.useState(false);
  // Prefill handed from the stream browser to the add dialog. The `nonce`
  // bumps every time we set it so the AddImageDialog's `key` changes and
  // react-hook-form re-mounts with the fresh defaults.
  const [prefill, setPrefill] = React.useState<{ alias: string; nonce: number }>({
    alias: "",
    nonce: 0,
  });

  const pickFromStream = (alias: string) => {
    setBrowseOpen(false);
    setPrefill({ alias, nonce: prefill.nonce + 1 });
    setAddOpen(true);
  };

  return (
    <div className="space-y-4" data-testid="page-compute-images">
      <header className="flex flex-wrap items-end justify-between gap-2">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold">{t("compute.images.title")}</h1>
          <p className="text-sm text-muted-foreground">{t("compute.images.subtitle")}</p>
        </div>
        {canCreate && (
          <div className="flex items-center gap-2">
            <Button variant="outline" onClick={() => setBrowseOpen(true)}>
              <SearchIcon className="h-4 w-4" />
              {t("compute.images.browse")}
            </Button>
            <Button
              onClick={() => {
                setPrefill({ alias: "", nonce: prefill.nonce + 1 });
                setAddOpen(true);
              }}
            >
              <PlusIcon className="h-4 w-4" />
              {t("compute.images.new")}
            </Button>
          </div>
        )}
      </header>

      <ImageTableBody
        loading={query.isLoading}
        error={query.error}
        images={query.data ?? []}
        canDelete={canDelete}
        onDelete={(id) => destroy.mutate({ imageId: id })}
      />

      <ImageStreamBrowser open={browseOpen} onOpenChange={setBrowseOpen} onPick={pickFromStream} />

      <AddImageDialog
        key={prefill.nonce}
        open={addOpen}
        onOpenChange={setAddOpen}
        tenantId={tenantId}
        defaultAlias={prefill.alias}
        onBrowse={() => {
          setAddOpen(false);
          setBrowseOpen(true);
        }}
      />
    </div>
  );
}

interface ImageTableBodyProps {
  loading: boolean;
  error: unknown;
  images: ComputeImage[];
  canDelete: boolean;
  onDelete: (id: string) => void;
}

function ImageTableBody({ loading, error, images, canDelete, onDelete }: ImageTableBodyProps) {
  const { t } = useTranslation();
  if (loading) return <LoadingState rows={4} />;
  if (error) {
    if (isFeatureDisabledError(error)) {
      return (
        <FeatureDisabledState
          title={t("common.featureDisabled.title")}
          description={t("common.featureDisabled.description")}
        />
      );
    }
    return <ErrorState message={t("common.error")} retryLabel={t("common.retry")} />;
  }
  if (images.length === 0) {
    return (
      <EmptyState
        icon={ImageIcon}
        title={t("compute.images.empty.title")}
        description={t("compute.images.empty.body")}
      />
    );
  }
  return (
    <div className="rounded-md border border-border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("compute.images.columns.alias")}</TableHead>
            <TableHead>{t("compute.images.columns.source")}</TableHead>
            <TableHead>{t("compute.images.columns.type")}</TableHead>
            <TableHead>{t("compute.images.columns.arch")}</TableHead>
            <TableHead>{t("compute.images.columns.fingerprint")}</TableHead>
            <TableHead className="text-end">{t("common.actions")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {images.map((img) => (
            <TableRow key={img.id}>
              <TableCell className="font-mono text-xs">{img.alias}</TableCell>
              <TableCell>
                <Badge variant="outline">{t(`compute.images.source.${img.source}`)}</Badge>
              </TableCell>
              <TableCell className="text-xs">{img.type ?? "—"}</TableCell>
              <TableCell className="text-xs">{img.architecture ?? "—"}</TableCell>
              <TableCell className="max-w-[200px] truncate font-mono text-xs">
                {img.fingerprint ?? "—"}
              </TableCell>
              <TableCell className="text-end">
                {img.source === "custom" && canDelete ? (
                  <Button
                    size="icon"
                    variant="ghost"
                    aria-label={t("common.delete")}
                    onClick={() => onDelete(img.id)}
                  >
                    <Trash2Icon className="h-4 w-4" />
                  </Button>
                ) : null}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}

interface AddImageDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  tenantId: string | null;
  defaultAlias: string;
  onBrowse: () => void;
}

function AddImageDialog({
  open,
  onOpenChange,
  tenantId,
  defaultAlias,
  onBrowse,
}: AddImageDialogProps) {
  const { t } = useTranslation();
  const upload = useUploadComputeImage(tenantId);

  const form = useForm<AddValues>({
    resolver: zodResolver(addSchema),
    defaultValues: {
      alias: defaultAlias,
      fingerprint: "",
      type: "container",
      architecture: "amd64",
    },
  });

  const onSubmit = form.handleSubmit(async (values) => {
    await upload.mutateAsync(values);
    onOpenChange(false);
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("compute.images.add.title")}</DialogTitle>
          <DialogDescription>{t("compute.images.add.subtitle")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="img-alias">{t("compute.images.add.alias.label")}</Label>
            <Input
              id="img-alias"
              placeholder={t("compute.images.add.alias.placeholder")}
              {...form.register("alias")}
            />
            {form.formState.errors.alias && (
              <p className="text-xs text-destructive">{form.formState.errors.alias.message}</p>
            )}
            <p className="text-xs text-muted-foreground">{t("compute.images.add.alias.hint")}</p>
          </div>
          <div className="space-y-2">
            <Label htmlFor="img-fp">{t("compute.images.add.fingerprint.label")}</Label>
            <Input
              id="img-fp"
              placeholder={t("compute.images.add.fingerprint.placeholder")}
              {...form.register("fingerprint")}
            />
            {form.formState.errors.fingerprint && (
              <p className="text-xs text-destructive">
                {form.formState.errors.fingerprint.message}
              </p>
            )}
            <button
              type="button"
              className="text-xs text-primary hover:underline"
              onClick={onBrowse}
            >
              {t("compute.images.add.browseForFingerprint")}
            </button>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-2">
              <Label htmlFor="img-type">{t("compute.images.add.type.label")}</Label>
              <Select
                value={form.watch("type")}
                onValueChange={(v) => form.setValue("type", v as "container" | "virtual-machine")}
              >
                <SelectTrigger id="img-type">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={INSTANCE_TYPES[0]}>{t("compute.create.type.container")}</SelectItem>
                  <SelectItem value={INSTANCE_TYPES[1]}>{t("compute.create.type.vm")}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label htmlFor="img-arch">{t("compute.images.add.arch.label")}</Label>
              <Input
                id="img-arch"
                placeholder={t("compute.images.add.arch.placeholder")}
                {...form.register("architecture")}
              />
            </div>
          </div>
          <div className="space-y-2">
            <Label htmlFor="img-desc">{t("compute.images.add.description.label")}</Label>
            <Textarea id="img-desc" rows={2} {...form.register("description")} />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={upload.isPending}>
              {upload.isPending ? t("common.saved") : t("compute.images.add.submit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
