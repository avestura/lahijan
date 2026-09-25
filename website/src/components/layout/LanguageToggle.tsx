/**
 * LanguageToggle — locale switcher (en / fa).
 *
 * Required by ADR-0017 ("Locale switcher UI: from day 1"). The actual
 * <html lang/dir> update is handled inside useLocale.
 */
import { GlobeIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useLocale } from "@/hooks/useLocale";
import { LOCALE_LABELS, SUPPORTED_LOCALES } from "@/lib/i18n";

export function LanguageToggle() {
  const { t } = useTranslation();
  const { locale, setLocale } = useLocale();

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="icon" aria-label={t("locale.label")}>
          <GlobeIcon className="h-5 w-5" aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>{t("locale.label")}</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={locale} onValueChange={(v) => setLocale(v as typeof locale)}>
          {SUPPORTED_LOCALES.map((l) => (
            <DropdownMenuRadioItem key={l} value={l} lang={l} data-active={locale === l}>
              {LOCALE_LABELS[l]}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
