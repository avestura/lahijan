/**
 * ImageAliasField — the image picker on the Create Instance wizard.
 *
 * Three ways to choose an image, so the picker is never a dead end:
 *   1. The tenant's catalog (featured + custom) as a dropdown.
 *   2. A live browser of images.linuxcontainers.org (searchable dialog).
 *   3. Manual alias entry (type any Incus-resolvable alias, e.g.
 *      "ubuntu/24.04" — Incus downloads it on demand at create time).
 *
 * The field is controlled: the parent form owns `value` + `onChange`.
 */
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { SearchIcon } from "lucide-react";
import type { components } from "@api-schema";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ImageStreamBrowser } from "./ImageStreamBrowser";

type ComputeImage = components["schemas"]["ComputeImage"];

// Sentinel value for the catalog dropdown's "custom alias" entry. Held in a
// const so the JSX `value=` prop reads an identifier (the i18n lint rule
// blocks bare string literals in user-facing props even for control values).
const CUSTOM_SENTINEL = "__custom__";

interface Props {
  value: string;
  onChange: (alias: string) => void;
  catalog: ComputeImage[];
  catalogLoading: boolean;
  error?: string;
}

export function ImageAliasField({ value, onChange, catalog, catalogLoading, error }: Props) {
  const { t } = useTranslation();
  const [browseOpen, setBrowseOpen] = useState(false);

  const inCatalog = useMemo(
    () => catalog.some((img) => img.alias === value),
    [catalog, value],
  );

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <Label htmlFor="imageAlias">{t("compute.create.image.label")}</Label>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => setBrowseOpen(true)}
          data-testid="create-instance-image-browse"
        >
          <SearchIcon className="h-4 w-4" />
          {t("compute.create.image.browse")}
        </Button>
      </div>

      {/* When the catalog has entries, offer it as a quick dropdown. The
          manual input below still lets the user type anything. */}
      {catalog.length > 0 && (
        <Select
          value={inCatalog ? value : CUSTOM_SENTINEL}
          onValueChange={(v) => v !== CUSTOM_SENTINEL && onChange(v)}
        >
          <SelectTrigger id="imageAlias">
            <SelectValue placeholder={t("compute.create.image.placeholder")} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={CUSTOM_SENTINEL}>
              {value ? `✏️ ${value}` : t("compute.create.image.custom")}
            </SelectItem>
            {catalog.map((img) => (
              <SelectItem key={img.id} value={img.alias}>
                <span className="flex items-center gap-2">
                  {img.alias}
                  <Badge variant="outline" className="text-[10px]">
                    {t(`compute.images.source.${img.source}`)}
                  </Badge>
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}

      <Input
        id="imageAlias-manual"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={t("compute.create.image.placeholder")}
        aria-label={t("compute.create.image.label")}
        data-testid="create-instance-image"
      />
      <p className="text-xs text-muted-foreground">{t("compute.create.image.hint")}</p>
      {catalogLoading && (
        <p className="text-xs text-muted-foreground">{t("common.loading")}</p>
      )}
      {error && <p className="text-xs text-destructive">{error}</p>}

      <ImageStreamBrowser
        open={browseOpen}
        onOpenChange={setBrowseOpen}
        onPick={(alias) => {
          onChange(alias);
          setBrowseOpen(false);
        }}
      />
    </div>
  );
}
