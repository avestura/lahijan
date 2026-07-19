/**
 * BrandMark — the Lahijan wordmark + simple logomark.
 *
 * Until the project has a finalized brand identity (see WS-19 open question
 * 1), we render a placeholder mark consistent with the favicon. Operators
 * can swap this component (or override `app.name` in the locale bundle) to
 * match their own deployment.
 */
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { cn } from "@/lib/utils";

interface BrandMarkProps {
  withWordmark?: boolean;
  className?: string;
}

export function BrandMark({ withWordmark = true, className }: BrandMarkProps) {
  const { t } = useTranslation();
  return (
    <Link
      to="/"
      className={cn("inline-flex items-center gap-2 rounded-md", className)}
      aria-label={t("app.name")}
    >
      <BrandGlyph className="h-8 w-8" />
      {withWordmark ? (
        <span className="text-base font-semibold tracking-tight text-foreground">
          {t("app.name")}
        </span>
      ) : null}
    </Link>
  );
}

export function BrandGlyph({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 64 64"
      role="img"
      aria-hidden="true"
      className={cn("text-primary", className)}
    >
      <rect width="64" height="64" rx="14" fill="currentColor" />
      <path d="M20 14 h8 v28 h18 v8 h-26 z" fill="hsl(var(--color-primary-fg))" />
      <circle cx="46" cy="18" r="5" fill="hsl(172 66% 50%)" />
    </svg>
  );
}
