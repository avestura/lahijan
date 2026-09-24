/**
 * Button — shadcn/ui primitive (Radix Slot + class-variance-authority),
 * restyled to the Boxy button spec (components-core.md).
 *
 * Square, 1px line, 80ms linear colour transitions, never an opacity fade.
 * Heights follow the control scale: sm 32 / md 40 / lg 48, icon buttons are
 * squares at the same height. One `default` (accent) button per view;
 * everything else is `outline` / `ghost`. `contrast` is the filled action
 * inside the inverted CTA band (ink on canvas, flips with the scope).
 *
 * Docs: https://ui.shadcn.com/docs/components/button
 */
/* eslint-disable react-refresh/only-export-components -- shadcn primitive pattern: variants are exported alongside the component */
import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";

import { cn } from "@/lib/utils";

const buttonVariants = cva(
  [
    "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap border font-sans text-base font-medium leading-none no-underline",
    "transition-colors duration-80 ease-linear hover:no-underline",
    "disabled:cursor-not-allowed disabled:border-line-subtle disabled:bg-surface-sunken disabled:text-ink-faint",
    "[&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0",
  ].join(" "),
  {
    variants: {
      variant: {
        default:
          "border-primary bg-primary text-primary-foreground hover:border-primary-hover hover:bg-primary-hover active:bg-primary-active",
        destructive:
          "border-destructive bg-destructive text-destructive-foreground hover:border-destructive-hover hover:bg-destructive-hover",
        outline:
          "border-line bg-surface text-ink hover:border-line-strong hover:bg-surface-hover active:bg-surface-active",
        secondary:
          "border-line bg-surface text-ink hover:border-line-strong hover:bg-surface-hover active:bg-surface-active",
        ghost:
          "border-transparent bg-transparent text-ink-muted hover:bg-surface-hover hover:text-ink active:bg-surface-active",
        contrast:
          "border-foreground bg-foreground text-background hover:border-ink-muted hover:bg-ink-muted",
        link: "border-transparent px-0 text-ink-accent underline-offset-[3px] hover:underline",
      },
      size: {
        default: "h-10 px-4",
        sm: "h-8 px-3 text-sm",
        lg: "h-12 px-6",
        icon: "h-10 w-10",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : "button";
    return (
      <Comp ref={ref} className={cn(buttonVariants({ variant, size }), className)} {...props} />
    );
  },
);
Button.displayName = "Button";

/**
 * ButtonGroup — shared-border group: buttons butt against each other and the
 * 1px gap shows the line colour, so two borders never double up.
 */
function ButtonGroup({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn("flex w-fit gap-px border border-line bg-line [&>*]:border-0", className)}
      {...props}
    />
  );
}

export { Button, ButtonGroup, buttonVariants };
