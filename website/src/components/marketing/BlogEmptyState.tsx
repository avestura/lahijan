/**
 * BlogEmptyState — shown on /blog and /blog/:slug until posts exist.
 *
 * A bordered sunken block with a mono label and one line of copy: an empty
 * state, not a dashed floating card.
 */
import { NewspaperIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

export function BlogEmptyState() {
  const { t } = useTranslation();
  return (
    <div className="flex items-start gap-6 border border-line bg-surface-sunken p-8">
      <span className="flex h-10 w-10 shrink-0 items-center justify-center border border-line bg-surface">
        <NewspaperIcon className="h-5 w-5" strokeWidth={1.5} aria-hidden="true" />
      </span>
      <div>
        <p className="bx-label">{t("page.blog.title")}</p>
        <p className="mt-2 text-md text-foreground">{t("page.blog.empty")}</p>
      </div>
    </div>
  );
}
