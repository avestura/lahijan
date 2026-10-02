/**
 * BrandMark — the Lahijan wordmark + logomark.
 *
 * The mark is a square ink tile with a tea cup (Lahijan is Iran's tea city)
 * knocked out, its top shaped as a cloud, standing on an accent saucer line.
 * Operators can swap this component (or override `app.name` in the locale
 * bundle) to match their own deployment.
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
      className={cn(
        "inline-flex items-center gap-3 text-foreground no-underline hover:no-underline",
        className,
      )}
      aria-label={t("app.name")}
    >
      <BrandGlyph className="h-8 w-8" />
      {withWordmark ? (
        <span className="font-display text-lg font-semibold tracking-heading text-foreground">
          {t("app.name")}
        </span>
      ) : null}
    </Link>
  );
}

export function BrandGlyph({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 64 64" role="img" aria-hidden="true" className={cn("shrink-0", className)}>
      <rect width="64" height="64" className="fill-foreground" />
      {/* A tea cup whose top is a cloud, on an accent saucer line. */}
      <g className="fill-background">
        <circle cx="20" cy="26" r="8" />
        <circle cx="31" cy="20" r="11" />
        <circle cx="42" cy="27" r="7" />
        <path d="M12 26H49V38Q49 50 37 50H24Q12 50 12 38Z" />
      </g>
      <path
        d="M49 30H52Q57 30 57 36Q57 42 51 42H48"
        className="fill-none stroke-background"
        strokeWidth="3"
      />
      <rect x="12" y="34" width="37" height="2" className="fill-foreground" />
      <rect x="8" y="53" width="46" height="3" className="fill-primary" />
    </svg>
  );
}
