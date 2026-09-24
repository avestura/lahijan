/**
 * Locale-aware number formatting for figures shown on the marketing site
 * (metric tiles, section numbers). Persian renders Persian digits.
 */
import { useTranslation } from "react-i18next";

/** formatNumber formats `n` for `locale`, optionally zero-padded to `minDigits`. */
export function formatNumber(n: number, locale: string, minDigits = 1): string {
  return new Intl.NumberFormat(locale, {
    minimumIntegerDigits: minDigits,
    useGrouping: false,
  }).format(n);
}

/** useFormatNumber binds formatNumber to the active i18next language. */
export function useFormatNumber(): (n: number, minDigits?: number) => string {
  const { i18n } = useTranslation();
  const locale = i18n.resolvedLanguage ?? i18n.language ?? "en";
  return (n, minDigits = 1) => formatNumber(n, locale, minDigits);
}
