/**
 * DropdownMenu — shadcn/ui primitive (wraps @radix-ui/react-dropdown-menu),
 * styled to the Boxy menu spec (components-overlays.md).
 *
 * - Floating surface: raised, 1px heavy line, hard 2px offset, 4px drop-in,
 *   and a raised scope (`bx-raised`) so hover stays visible in dark mode.
 * - Items: full-bleed 32px rows, 12px icon gap, icons subtle at rest and ink
 *   when highlighted. Radix moves focus with the pointer, so there is only
 *   ever one highlighted item; keyboard use adds a 2px accent bar.
 * - Checkable items reserve a 16px glyph slot (drawn check / 6px square).
 * - Submenus hang flush off the parent's inline-end rule, first item level
 *   with the trigger, and fade in over 80ms with no translate.
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
const DropdownMenuSub = DropdownMenuPrimitive.Sub;
const DropdownMenuRadioGroup = DropdownMenuPrimitive.RadioGroup;

const SURFACE =
  "bx-raised z-50 min-w-[216px] overflow-hidden border border-line-heavy bg-popover py-1 text-popover-foreground shadow-hard-2";

// `bx-menu-item` carries the keyboard-only accent bar (tailwind.css), keyed
// off <html data-input> rather than :focus-visible - Radix moves real focus
// with the pointer, so the browser cannot tell the two inputs apart.
const ITEM = cn(
  "bx-menu-item relative flex h-8 w-full cursor-default select-none items-center gap-3 whitespace-nowrap px-3 text-sm text-ink outline-none",
  "transition-[background-color,color,box-shadow] duration-75 ease-linear",
  "[&>svg]:me-0 [&>svg]:size-4 [&>svg]:shrink-0 [&>svg]:text-ink-subtle",
  "data-[highlighted]:bg-surface-hover data-[highlighted]:[&>svg]:text-ink",
  "data-[disabled]:pointer-events-none data-[disabled]:text-ink-faint data-[disabled]:[&>svg]:text-ink-faint",
);

const ITEM_DANGER = cn(
  "bx-menu-item--danger text-destructive-ink [&>svg]:text-current",
  "data-[highlighted]:bg-destructive-soft data-[highlighted]:text-destructive-ink data-[highlighted]:[&>svg]:text-current",
);

const DropdownMenuSubTrigger = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.SubTrigger>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.SubTrigger> & {
    inset?: boolean;
  }
>(({ className, inset, children, ...props }, ref) => (
  <DropdownMenuPrimitive.SubTrigger
    ref={ref}
    className={cn(
      ITEM,
      "data-[state=open]:bg-surface-hover data-[state=open]:[&>svg]:text-ink",
      inset && "ps-10",
      className,
    )}
    {...props}
  >
    {children}
    <span className="bx-chevron ms-auto" aria-hidden="true" />
  </DropdownMenuPrimitive.SubTrigger>
));
DropdownMenuSubTrigger.displayName = DropdownMenuPrimitive.SubTrigger.displayName;

const DropdownMenuSubContent = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.SubContent>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.SubContent>
>(({ className, sideOffset = 0, alignOffset = -5, ...props }, ref) => (
  <DropdownMenuPrimitive.Portal>
    <DropdownMenuPrimitive.SubContent
      ref={ref}
      // Flush: the child's 1px border lands on the parent's inline-end rule
      // (sideOffset 0), and -(4px padding + 1px border) levels its first item
      // with the trigger.
      sideOffset={sideOffset}
      alignOffset={alignOffset}
      className={cn(SURFACE, "data-[state=open]:animate-fade-fast", className)}
      {...props}
    />
  </DropdownMenuPrimitive.Portal>
));
DropdownMenuSubContent.displayName = DropdownMenuPrimitive.SubContent.displayName;

const DropdownMenuContent = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Content>
>(({ className, sideOffset = 4, collisionPadding = 8, ...props }, ref) => (
  <DropdownMenuPrimitive.Portal>
    <DropdownMenuPrimitive.Content
      ref={ref}
      sideOffset={sideOffset}
      collisionPadding={collisionPadding}
      className={cn(SURFACE, "data-[state=open]:animate-drop", className)}
      {...props}
    />
  </DropdownMenuPrimitive.Portal>
));
DropdownMenuContent.displayName = DropdownMenuPrimitive.Content.displayName;

type ItemProps = React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.Item> & {
  inset?: boolean;
  /** Destructive command: danger ink, danger-soft highlight, danger focus bar. */
  variant?: "default" | "destructive";
};

const DropdownMenuItem = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.Item>,
  ItemProps
>(({ className, inset, variant = "default", ...props }, ref) => (
  <DropdownMenuPrimitive.Item
    ref={ref}
    className={cn(ITEM, variant === "destructive" && ITEM_DANGER, inset && "ps-10", className)}
    {...props}
  />
));
DropdownMenuItem.displayName = DropdownMenuPrimitive.Item.displayName;

const DropdownMenuCheckboxItem = React.forwardRef<
  React.ElementRef<typeof DropdownMenuPrimitive.CheckboxItem>,
  React.ComponentPropsWithoutRef<typeof DropdownMenuPrimitive.CheckboxItem>
>(({ className, children, checked, ...props }, ref) => (
  <DropdownMenuPrimitive.CheckboxItem
    ref={ref}
    className={cn(ITEM, "data-[state=checked]:font-medium", className)}
    checked={checked}
    {...props}
  >
    <span className="bx-menu-check" aria-hidden="true" />
    {children}
  </DropdownMenuPrimitive.CheckboxItem>
));
DropdownMenuCheckboxItem.displayName = DropdownMenuPrimitive.CheckboxItem.displayName;

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
      "flex h-7 items-center px-3 font-mono text-2xs font-medium uppercase tracking-[0.08em] text-ink-subtle",
      inset && "ps-10",
      className,
    )}
    {...props}
  />
));
DropdownMenuLabel.displayName = DropdownMenuPrimitive.Label.displayName;

/**
 * Head strip: sunken, ruled, flush to the surface edge — for context such as
 * an account menu's avatar, name and email. Place it first in the content.
 */
const DropdownMenuHead = ({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cn(
      "-mt-1 mb-1 flex items-center gap-3 border-b border-line-subtle bg-surface-sunken p-3",
      className,
    )}
    {...props}
  />
);
DropdownMenuHead.displayName = "DropdownMenuHead";

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

/** Shortcut hint: mono 11px at the inline end, 32px clear of the label. */
const DropdownMenuShortcut = ({ className, ...props }: React.HTMLAttributes<HTMLSpanElement>) => (
  <span
    className={cn("ms-auto ps-8 font-mono text-label tracking-[0.04em] text-ink-subtle", className)}
    {...props}
  />
);
DropdownMenuShortcut.displayName = "DropdownMenuShortcut";

export {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuCheckboxItem,
  DropdownMenuRadioItem,
  DropdownMenuLabel,
  DropdownMenuHead,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuGroup,
  DropdownMenuPortal,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuRadioGroup,
};
