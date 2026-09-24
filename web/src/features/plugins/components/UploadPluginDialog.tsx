/**
 * UploadPluginDialog — upload one `.lahx` extension package.
 *
 * A `.lahx` file is a ZIP holding `lahijan.manifest.yaml` + `plugin.wasm`
 * at its root (backend: internal/app/lahijan/wasm/lahx). The admin picks
 * or drops that single file; the server unpacks, validates and installs
 * it in "pending" state.
 */
import * as React from "react";
import { useTranslation } from "react-i18next";
import { PackageIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import { useUploadAdminPlugin } from "../api";
import { EXTENSION_PACKAGE_SUFFIX } from "../schemas";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** isExtensionPackage — the client-side check mirrors the server's. */
function isExtensionPackage(file: File): boolean {
  return file.name.toLowerCase().endsWith(EXTENSION_PACKAGE_SUFFIX);
}

export function UploadPluginDialog({ open, onOpenChange }: Props) {
  const { t } = useTranslation();
  const upload = useUploadAdminPlugin();
  const inputRef = React.useRef<HTMLInputElement | null>(null);
  const [pkg, setPkg] = React.useState<File | null>(null);
  const [wrongType, setWrongType] = React.useState(false);
  const [dragging, setDragging] = React.useState(false);

  const choose = (file: File | null | undefined) => {
    if (!file) return;
    if (!isExtensionPackage(file)) {
      setPkg(null);
      setWrongType(true);
      return;
    }
    setWrongType(false);
    setPkg(file);
  };

  const reset = () => {
    setPkg(null);
    setWrongType(false);
    if (inputRef.current) inputRef.current.value = "";
  };

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!pkg) return;
    try {
      await upload.mutateAsync({ pkg });
      onOpenChange(false);
      reset();
    } catch {
      /* toast handled by the hook */
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) reset();
        onOpenChange(next);
      }}
    >
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("plugins.upload.title")}</DialogTitle>
          <DialogDescription>{t("plugins.upload.subtitle")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="plugin-package">{t("plugins.upload.package.label")}</Label>
            <div
              role="button"
              tabIndex={0}
              data-testid="plugin-package-dropzone"
              onClick={() => inputRef.current?.click()}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  inputRef.current?.click();
                }
              }}
              onDragOver={(e) => {
                e.preventDefault();
                setDragging(true);
              }}
              onDragLeave={() => setDragging(false)}
              onDrop={(e) => {
                e.preventDefault();
                setDragging(false);
                choose(e.dataTransfer.files[0]);
              }}
              className={cn(
                "flex cursor-pointer flex-col items-center gap-2 border border-dashed border-border p-6 text-center",
                dragging && "border-primary bg-muted",
              )}
            >
              <PackageIcon className="h-6 w-6 text-muted-foreground" aria-hidden />
              <p className="text-sm">{t("plugins.upload.package.drop")}</p>
              <p className="text-xs text-muted-foreground">{t("plugins.upload.package.hint")}</p>
            </div>
            <input
              ref={inputRef}
              id="plugin-package"
              data-testid="plugin-package-input"
              type="file"
              accept={`${EXTENSION_PACKAGE_SUFFIX},application/zip`}
              className="sr-only"
              onChange={(e) => choose(e.target.files?.[0])}
            />
            {pkg && (
              <p className="font-mono text-xs" data-testid="plugin-package-selected">
                {t("plugins.upload.summary.package", { name: pkg.name, size: pkg.size })}
              </p>
            )}
            {wrongType && (
              <p role="alert" className="text-xs text-destructive">
                {t("plugins.upload.package.wrongType")}
              </p>
            )}
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={upload.isPending || !pkg}>
              {upload.isPending ? t("plugins.upload.submitting") : t("plugins.upload.submit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
