/**
 * react-i18next wiring for the Lahijan dashboard.
 *
 * - Two locales: `en` (default, LTR) and `fa` (Persian, RTL).
 * - Locales are statically imported so they bundle together (no async
 *   loading churn for two small JSON files).
 * - LanguageDetector reads from localStorage (so the user's pick survives a
 *   reload) with `<html lang>` + navigator.language as fallbacks.
 *
 * Per ADR-0017 every user-visible string passes through `t("key")` from this
 * instance. The lint rule (@lahijan/i18n/no-jsx-literal-strings) blocks bare
 * English literals at compile time.
 */
import i18n from "i18next";
import LanguageDetector from "i18next-browser-languagedetector";
import { initReactI18next } from "react-i18next";

import en from "../locales/en.json";
import fa from "../locales/fa.json";

export const SUPPORTED_LOCALES = ["en", "fa"] as const;
export type Locale = (typeof SUPPORTED_LOCALES)[number];

export const DEFAULT_LOCALE: Locale = "en";

export const LOCALE_DIRECTIONS: Record<Locale, "ltr" | "rtl"> = {
  en: "ltr",
  fa: "rtl",
};

export const LOCALE_LABELS: Record<Locale, string> = {
  en: "English",
  fa: "فارسی",
};

/**
 * isRTL returns true for RTL locales (currently fa). Used by the theme
 * bootstrap to set <html dir> on load.
 */
export function isRTL(locale: string): boolean {
  return LOCALE_DIRECTIONS[locale as Locale] === "rtl";
}

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      en: { translation: en },
      fa: { translation: fa },
    },
    fallbackLng: DEFAULT_LOCALE,
    supportedLngs: [...SUPPORTED_LOCALES],
    nonExplicitSupportedLngs: true,
    interpolation: {
      // React already escapes; no need for i18next's escaping.
      escapeValue: false,
    },
    detection: {
      order: ["localStorage", "htmlTag", "navigator"],
      lookupLocalStorage: "lahijan.locale",
      caches: ["localStorage"],
    },
    react: {
      // Suspend until the bundle is ready; the AppShell is wrapped in
      // <Suspense> at the router root.
      useSuspense: true,
    },
  });

export default i18n;
