/**
 * LocaleToggle — language selector (en / fa).
 *
 * Mounted in the Header. Persists via i18next's LanguageDetector
 * (localStorage: lahijan.locale). The <html lang> + <html dir> attributes
 * flip via the locale subscription in src/main.tsx.
 */
import { GlobeIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useLocale } from "@/hooks/useLocale";
import { LOCALE_LABELS, SUPPORTED_LOCALES } from "@/lib/i18n";

export function LocaleToggle() {
  const { t } = useTranslation();
  const { locale, setLocale } = useLocale();

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          aria-label={t("locale.label")}
          data-testid="locale-toggle"
        >
          <GlobeIcon className="h-4 w-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {/* A picker: radio items reserve the mark slot; choosing closes it. */}
        <DropdownMenuRadioGroup value={locale} onValueChange={(v) => setLocale(v as typeof locale)}>
          {SUPPORTED_LOCALES.map((code) => (
            <DropdownMenuRadioItem
              key={code}
              value={code}
              data-active={locale === code}
              data-testid={`locale-option-${code}`}
            >
              {LOCALE_LABELS[code]}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
