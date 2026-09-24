/**
 * Badge — shadcn/ui primitive (class-variance-authority).
 *
 * Docs: https://ui.shadcn.com/docs/components/badge
 */
/* eslint-disable react-refresh/only-export-components -- shadcn primitive pattern: variants are exported alongside the component */
import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

// Boxy tag (components-core.md): square, 1px border, mono uppercase, never
// a pill. Semantic variants use the soft fill + base border + semantic ink
// trio; color is always paired with the text label.
const badgeVariants = cva(
  "inline-flex h-5 items-center gap-1 whitespace-nowrap border px-2 font-mono text-label font-medium uppercase tracking-[0.08em]",
  {
    variants: {
      variant: {
        default: "border-line-accent bg-primary-soft text-ink-accent",
        secondary: "border-line bg-surface-sunken text-ink-muted",
        destructive: "border-destructive bg-destructive-soft text-destructive-ink",
        success: "border-success bg-success-soft text-success-ink",
        warning: "border-warning bg-warning-soft text-warning-ink",
        outline: "border-line bg-transparent text-ink-muted",
        muted: "border-line-subtle bg-surface-sunken text-ink-subtle",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

export interface BadgeProps
  extends React.HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof badgeVariants> {}

/**
 * Badge — square status tag. Use for instance states, role tags, etc.
 */
const Badge = React.forwardRef<HTMLSpanElement, BadgeProps>(
  ({ className, variant, ...props }, ref) => (
    <span ref={ref} className={cn(badgeVariants({ variant }), className)} {...props} />
  ),
);
Badge.displayName = "Badge";

export { Badge, badgeVariants };
