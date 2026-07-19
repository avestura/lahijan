/**
 * UploadPluginDialog — upload .wasm + manifest via multipart/form-data.
 *
 * Per WS-21 open question default: drag-drop AND file picker. Both
 * inputs accept the same MIME-ish set; the manifest textarea accepts
 * pasted YAML too.
 */
import * as React from "react";
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
import { useUploadAdminPlugin } from "../api";
import { MANIFEST_SAMPLE } from "../schemas";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function UploadPluginDialog({ open, onOpenChange }: Props) {
  const { t } = useTranslation();
  const upload = useUploadAdminPlugin();
  const [wasm, setWasm] = React.useState<File | null>(null);
  const [manifest, setManifest] = React.useState<string>("");

  const onSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!wasm || !manifest.trim()) return;
    try {
      await upload.mutateAsync({ wasm, manifest });
      onOpenChange(false);
      setWasm(null);
      setManifest("");
    } catch {
      /* toast handled by the hook */
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("plugins.upload.title")}</DialogTitle>
          <DialogDescription>{t("plugins.upload.subtitle")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={onSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="wasm-file">{t("plugins.upload.wasm.label")}</Label>
            <Input
              id="wasm-file"
              type="file"
              accept=".wasm,application/wasm"
              onChange={(e) => setWasm(e.target.files?.[0] ?? null)}
            />
            <p className="text-xs text-muted-foreground">{t("plugins.upload.wasm.hint")}</p>
            {wasm && (
              <p className="font-mono text-xs">
                {t("plugins.upload.summary.wasm")}: {`${wasm.name} (${wasm.size} bytes)`}
              </p>
            )}
          </div>
          <div className="space-y-2">
            <Label htmlFor="manifest-text">{t("plugins.upload.manifest.label")}</Label>
            <Textarea
              id="manifest-text"
              rows={10}
              placeholder={MANIFEST_SAMPLE}
              value={manifest}
              onChange={(e) => setManifest(e.target.value)}
              className="font-mono text-xs"
            />
            <p className="text-xs text-muted-foreground">{t("plugins.upload.manifest.hint")}</p>
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={upload.isPending || !wasm || !manifest.trim()}>
              {upload.isPending ? t("plugins.upload.submitting") : t("plugins.upload.submit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
