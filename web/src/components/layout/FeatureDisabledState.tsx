/**
 * FeatureDisabledState — placeholder shown when a backend module returns
 * 501 "feature disabled" (e.g. compute on Windows where Incus cannot
 * run, or storage when SeaweedFS is not wired).
 *
 * Visually similar to EmptyState but uses a muted "info" treatment
 * rather than the destructive ErrorState look — a disabled feature is
 * an expected state, not a failure.
 */
import { BanIcon } from "lucide-react";

import { cn } from "@/lib/utils";

interface FeatureDisabledStateProps {
  title: string;
  description?: string;
  className?: string;
}

export function FeatureDisabledState({ title, description, className }: FeatureDisabledStateProps) {
  return (
    <div
      role="status"
      data-testid="feature-disabled"
      className={cn(
        "flex flex-col items-center justify-center gap-3 border border-line bg-surface-sunken px-6 py-16 text-center",
        className,
      )}
    >
      <div className="flex h-12 w-12 items-center justify-center border border-line-strong text-ink-subtle">
        <BanIcon className="h-6 w-6" aria-hidden="true" />
      </div>
      <div className="space-y-1">
        <p className="text-base font-semibold text-ink">{title}</p>
        {description ? (
          <p className="mx-auto max-w-sm text-sm text-ink-muted">{description}</p>
        ) : null}
      </div>
    </div>
  );
}
