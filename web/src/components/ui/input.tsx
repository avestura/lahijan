/**
 * Input — shadcn/ui primitive.
 *
 * Docs: https://ui.shadcn.com/docs/components/input
 */
import * as React from "react";

import { cn } from "@/lib/utils";

export type InputProps = React.InputHTMLAttributes<HTMLInputElement>;

const Input = React.forwardRef<HTMLInputElement, InputProps>(
  ({ className, type, ...props }, ref) => {
    return (
      <input
        type={type}
        ref={ref}
        className={cn(
          // Industrial underlined field (components-core.md): sunken fill,
          // bottom rule only; focus draws a 2px accent underline without
          // shifting layout; invalid switches it to danger.
          "flex h-[var(--bx-control-md)] w-full border-0 border-b border-line-strong bg-surface-sunken px-3 text-sm text-ink",
          "transition-[border-color,box-shadow] duration-75 ease-linear",
          "file:border-0 file:bg-transparent file:text-sm file:font-medium",
          "placeholder:text-ink-subtle hover:border-ink-muted",
          "focus:border-transparent focus:shadow-[inset_0_-2px_0_0_var(--bx-accent)] focus:outline-none",
          "aria-[invalid=true]:shadow-[inset_0_-2px_0_0_var(--bx-danger)]",
          "disabled:cursor-not-allowed disabled:border-line-subtle disabled:text-ink-faint",
          className,
        )}
        {...props}
      />
    );
  },
);
Input.displayName = "Input";

export { Input };
