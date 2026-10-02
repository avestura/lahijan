/**
 * Pagination — previous / next controls with a "from–to of total" label,
 * for offset-paginated tables.
 */
import { useTranslation } from "react-i18next";
import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react";

import { Button } from "@/components/ui/button";

interface PaginationProps {
  offset: number;
  limit: number;
  total: number;
  /** Called with the new offset. */
  onChange: (offset: number) => void;
  disabled?: boolean;
}

export function Pagination({ offset, limit, total, onChange, disabled }: PaginationProps) {
  const { t } = useTranslation();
  if (total <= 0) return null;
  const from = offset + 1;
  const to = Math.min(offset + limit, total);
  return (
    <div className="flex items-center justify-between gap-3">
      <p className="text-xs text-muted-foreground" aria-live="polite">
        {t("common.pagination.range", { from, to, total })}
      </p>
      <div className="flex gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled === true || offset <= 0}
          onClick={() => onChange(Math.max(0, offset - limit))}
        >
          <ChevronLeftIcon className="h-4 w-4 rtl:rotate-180" />
          {t("common.previous")}
        </Button>
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={disabled === true || offset + limit >= total}
          onClick={() => onChange(offset + limit)}
        >
          {t("common.next")}
          <ChevronRightIcon className="h-4 w-4 rtl:rotate-180" />
        </Button>
      </div>
    </div>
  );
}
