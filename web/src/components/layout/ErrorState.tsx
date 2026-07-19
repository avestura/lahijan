/**
 * ErrorState — full-region "something went wrong" placeholder.
 *
 * Used when a query errors. Renders the localized "something went wrong"
 * message plus a retry button that calls `onRetry`.
 */
import { AlertTriangleIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

interface ErrorStateProps {
  message: string;
  retryLabel: string;
  onRetry?: () => void;
  className?: string;
}

export function ErrorState({ message, retryLabel, onRetry, className }: ErrorStateProps) {
  return (
    <div
      role="alert"
      className={cn(
        "flex flex-col items-center justify-center gap-3 rounded-xl border border-destructive/30 bg-destructive/5 p-10 text-center",
        className,
      )}
    >
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-destructive/10 text-destructive">
        <AlertTriangleIcon className="h-6 w-6" aria-hidden="true" />
      </div>
      <p className="max-w-md text-sm text-foreground">{message}</p>
      {onRetry ? (
        <Button variant="outline" size="sm" onClick={onRetry}>
          {retryLabel}
        </Button>
      ) : null}
    </div>
  );
}
