/**
 * Skeleton — shadcn/ui primitive.
 *
 * Renders a placeholder shimmer for loading states.
 *
 * Docs: https://ui.shadcn.com/docs/components/skeleton
 */
import * as React from "react";

import { cn } from "@/lib/utils";

/**
 * Skeleton — pulsing placeholder used while data is loading. Compose
 * multiple of these to mimic the shape of the real content.
 */
const Skeleton = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("animate-pulse rounded-md bg-muted", className)} {...props} />
  ),
);
Skeleton.displayName = "Skeleton";

export { Skeleton };
