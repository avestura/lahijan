/**
 * useLocale — selector around react-i18next's `i18n` instance.
 *
 * setLocale flips the language AND updates <html lang> + <html dir> in one
 * shot, so the RTL layout follows the locale without any extra wiring on
 * the consumer side. Mirrors web/src/hooks/useLocale.ts.
 */
import { useCallback } from "react";
import { useTranslation } from "react-i18next";

import { DEFAULT_LOCALE, isRTL, SUPPORTED_LOCALES, type Locale } from "@/lib/i18n";

function applyHtmlAttrs(locale: Locale) {
  if (typeof document === "undefined") return;
  const html = document.documentElement;
  html.lang = locale;
  html.dir = isRTL(locale) ? "rtl" : "ltr";
}

export function useLocale() {
  const { i18n } = useTranslation();
  const current = (i18n.resolvedLanguage ?? i18n.language ?? DEFAULT_LOCALE) as Locale;
  const locale: Locale = SUPPORTED_LOCALES.includes(current) ? current : DEFAULT_LOCALE;

  const setLocale = useCallback(
    (next: Locale) => {
      void i18n.changeLanguage(next);
      applyHtmlAttrs(next);
    },
    [i18n],
  );

  return { locale, setLocale };
}
