/**
 * BrandMark — the Lahijan wordmark + logomark.
 *
 * The mark is a square ink tile holding a smaller square split in two by a
 * thin gap: white hills below, and the accent-coloured sky that fills the
 * space around them above, like the mist over Gilan's tea country. Hard edges
 * only, to match the design system.
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
      {/* Two hills and the sky around them, split by a thin gap into one square. */}
      <path d="M8 8H56V53L42 39L34 47L24 37L8 53Z" className="fill-primary" />
      <path d="M8 56L24 40L34 50L42 42L56 56Z" className="fill-background" />
    </svg>
  );
}
