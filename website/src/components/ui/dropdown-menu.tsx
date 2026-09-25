/**
 * DropdownMenu — shadcn/ui primitive (wraps @radix-ui/react-dropdown-menu).
 *
 * Mirrors web/src/components/ui/dropdown-menu.tsx but only the subset used
 * by the marketing site (Theme + Language pickers), in this app's Tailwind
 * names (text-base = 14px, shadow-2).
 *
 * Boxy menu (components-overlays.md): a raised floating surface with a 1px
 * heavy line and a hard 2px offset, 4px drop-in; full-bleed 32px items with
 * a 12px icon gap, icons subtle at rest and ink when highlighted; keyboard
 * focus adds a 2px accent bar. Pickers use radio items: a reserved 16px slot
 * with a 6px square on the chosen option.
 *
 * Docs: https://ui.shadcn.com/docs/components/dropdown-menu
 */
import * as React from "react";
import * as DropdownMenuPrimitive from "@radix-ui/react-dropdown-menu";

import { cn } from "@/lib/utils";

const DropdownMenu = DropdownMenuPrimitive.Root;
const DropdownMenuTrigger = DropdownMenuPrimitive.Trigger;
const DropdownMenuGroup = DropdownMenuPrimitive.Group;
const DropdownMenuPortal = DropdownMenuPrimitive.Portal;
const DropdownMenuRadioGroup = DropdownMenuPrimitive.RadioGroup;

const ITEM = cn(
  "relative flex h-8 w-full cursor-default select-none items-center gap-3 whitespace-nowrap px-3 text-base text-ink outline-none",
  "transition-[background-color,color,box-shadow] duration-80 ease-linear",
  "[&>svg]:size-4 [&>svg]:shrink-0 [&>svg]:text-ink-subtle",
  "data-[highlighted]:bg-surface-hover data-[highlighted]:[&>svg]:text-ink",
  "focus-visible:shadow-[inset_2px_0_0_0_var(--bx-accent)] rtl:focus-visible:shadow-[inset_-2px_0_0_0_var(--bx-accent)]",
  "data-[disabled]:pointer-events-none data-[disabled]:text-ink-faint",
);

const DropdownMenuContent = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Content>
>(({ className, sideOffset = 4, collisionPadding = 8, ...props }, ref) => (
  <DropdownMenuPrimitive.Portal>
    <DropdownMenuPrimitive.Content
      ref={ref}
      sideOffset={sideOffset}
      collisionPadding={collisionPadding}
      className={cn(
        "bx-raised z-20 min-w-[216px] overflow-hidden border border-line-heavy bg-popover py-1 text-popover-foreground shadow-2",
        "data-[state=open]:animate-fade-in-down",
        className,
      )}
      {...props}
    />
  </DropdownMenuPrimitive.Portal>
));
DropdownMenuContent.displayName = DropdownMenuPrimitive.Content.displayName;

const DropdownMenuItem = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.Item>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Item> & {
    inset?: boolean;
  }
>(({ className, inset, ...props }, ref) => (
  <DropdownMenuPrimitive.Item
    ref={ref}
    className={cn(ITEM, inset && "ps-10", className)}
    {...props}
  />
));
DropdownMenuItem.displayName = DropdownMenuPrimitive.Item.displayName;

const DropdownMenuRadioItem = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.RadioItem>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.RadioItem>
>(({ className, children, ...props }, ref) => (
  <DropdownMenuPrimitive.RadioItem
    ref={ref}
    className={cn(ITEM, "data-[state=checked]:font-medium", className)}
    {...props}
  >
    <span className="bx-menu-radio" aria-hidden="true" />
    {children}
  </DropdownMenuPrimitive.RadioItem>
));
DropdownMenuRadioItem.displayName = DropdownMenuPrimitive.RadioItem.displayName;

/** Group label: mono 10px uppercase on a 28px row, no fill. */
const DropdownMenuLabel = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.Label>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Label> & {
    inset?: boolean;
  }
>(({ className, inset, ...props }, ref) => (
  <DropdownMenuPrimitive.Label
    ref={ref}
    className={cn(
      "flex h-7 items-center px-3 font-mono text-2xs font-medium uppercase tracking-label text-ink-subtle",
      inset && "ps-10",
      className,
    )}
    {...props}
  />
));
DropdownMenuLabel.displayName = DropdownMenuPrimitive.Label.displayName;

/** Separator: separates groups, not items; 4px clear above and below. */
const DropdownMenuSeparator = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.Separator>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Separator>
>(({ className, ...props }, ref) => (
  <DropdownMenuPrimitive.Separator
    ref={ref}
    className={cn("my-1 h-px bg-line-subtle", className)}
    {...props}
  />
));
DropdownMenuSeparator.displayName = DropdownMenuPrimitive.Separator.displayName;

export {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuGroup,
  DropdownMenuPortal,
};
