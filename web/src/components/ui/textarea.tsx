/**
 * Textarea — shadcn/ui primitive.
 *
 * Docs: https://ui.shadcn.com/docs/components/textarea
 */
import * as React from "react";

import { cn } from "@/lib/utils";

export type TextareaProps = React.TextareaHTMLAttributes<HTMLTextAreaElement>;

/**
 * Textarea — multi-line text input. Used for descriptions, config blobs,
 * audit metadata display, etc.
 */
const Textarea = React.forwardRef<HTMLTextAreaElement, TextareaProps>(
  ({ className, ...props }, ref) => (
    <textarea
      ref={ref}
      className={cn(
        // Same underlined treatment as Input (one field style per product).
        "flex min-h-24 w-full resize-y border-0 border-b border-line-strong bg-surface-sunken p-3 text-sm text-ink",
        "transition-[border-color,box-shadow] duration-75 ease-linear",
        "placeholder:text-ink-subtle hover:border-ink-muted",
        "focus:border-transparent focus:shadow-[inset_0_-2px_0_0_var(--bx-accent)] focus:outline-none",
        "aria-[invalid=true]:shadow-[inset_0_-2px_0_0_var(--bx-danger)]",
        "disabled:cursor-not-allowed disabled:border-line-subtle disabled:text-ink-faint",
        className,
      )}
      {...props}
    />
  ),
);
Textarea.displayName = "Textarea";

export { Textarea };
