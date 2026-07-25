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
        "flex flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-border bg-muted/30 p-10 text-center",
        className,
      )}
    >
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted text-muted-foreground">
        <BanIcon className="h-6 w-6" aria-hidden="true" />
      </div>
      <div className="space-y-1">
        <p className="text-base font-medium text-foreground">{title}</p>
        {description ? (
          <p className="mx-auto max-w-sm text-sm text-muted-foreground">{description}</p>
        ) : null}
      </div>
    </div>
  );
}
