/**
 * Tabs — shadcn/ui primitive (wraps @radix-ui/react-tabs), Boxy tab strip:
 * tabs butt together over one full-width 1px rule; the active tab's 2px
 * accent bar is drawn with shadows (never a negative margin) so a hover
 * fill can never punch a gap in the rule (components-core.md).
 *
 * Docs: https://ui.shadcn.com/docs/components/tabs
 */
import * as React from "react";
import * as TabsPrimitive from "@radix-ui/react-tabs";

import { cn } from "@/lib/utils";

const Tabs = TabsPrimitive.Root;

const TabsList = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.List>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.List>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.List
    ref={ref}
    className={cn(
      "flex w-full items-stretch overflow-x-auto border-b border-line text-ink-subtle",
      className,
    )}
    {...props}
  />
));
TabsList.displayName = TabsPrimitive.List.displayName;

const TabsTrigger = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Trigger
    ref={ref}
    className={cn(
      "inline-flex h-[var(--bx-control-md)] shrink-0 items-center justify-center whitespace-nowrap px-4 text-sm font-medium",
      "transition-[background-color,color] duration-75 ease-linear hover:bg-surface-hover hover:text-ink",
      "focus-visible:outline-offset-[-2px]",
      "disabled:cursor-not-allowed disabled:text-ink-faint",
      "data-[state=active]:text-ink data-[state=active]:shadow-[inset_0_-2px_0_0_var(--bx-accent),0_1px_0_0_var(--bx-accent)]",
      className,
    )}
    {...props}
  />
));
TabsTrigger.displayName = TabsPrimitive.Trigger.displayName;

const TabsContent = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Content>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Content
    ref={ref}
    className={cn("mt-5 animate-fade-in focus-visible:outline-offset-4", className)}
    {...props}
  />
));
TabsContent.displayName = TabsPrimitive.Content.displayName;

export { Tabs, TabsList, TabsTrigger, TabsContent };
