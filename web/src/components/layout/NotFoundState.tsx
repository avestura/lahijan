/**
 * NotFoundState — rendered by TanStack Router when no route matches.
 *
 * Mounted via the root route's `notFoundComponent`. Kept as its own
 * component so the e2e suite (WS-22) can target it with a stable
 * `data-testid` instead of matching on localized copy.
 */
import { useTranslation } from "react-i18next";
import { MapIcon } from "lucide-react";
import { Link } from "@tanstack/react-router";

import { Button } from "@/components/ui/button";

export function NotFoundState() {
  const { t } = useTranslation();
  return (
    <div
      role="alert"
      data-testid="not-found"
      className="flex flex-col items-center justify-center gap-4 p-16 text-center"
    >
      <div className="flex h-12 w-12 items-center justify-center border border-line-strong text-ink-subtle">
        <MapIcon className="h-6 w-6" aria-hidden="true" />
      </div>
      <div className="space-y-1">
        <p className="text-lg font-semibold">{t("common.notFound.title")}</p>
        <p className="mx-auto max-w-sm text-sm text-muted-foreground">
          {t("common.notFound.body")}
        </p>
      </div>
      <Button asChild>
        <Link to="/dashboard">{t("common.notFound.back")}</Link>
      </Button>
    </div>
  );
}
