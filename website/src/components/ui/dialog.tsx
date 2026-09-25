/**
 * Dialog — shadcn/ui primitive (wraps @radix-ui/react-dialog).
 *
 * Used by the marketing site's mobile navigation drawer. Boxy treatment: a
 * solid scrim (never blurred) that fades over 240ms, and a full-height
 * drawer on the inline-end edge with a heavy 1px line and a hard 4px offset
 * shadow. It slides in over 160ms with the sharp curve, mirrored in RTL.
 *
 * Docs: https://ui.shadcn.com/docs/components/dialog
 */
import * as React from "react";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import { XIcon } from "lucide-react";

import { cn } from "@/lib/utils";

const Dialog = DialogPrimitive.Root;
const DialogTrigger = DialogPrimitive.Trigger;
const DialogPortal = DialogPrimitive.Portal;
const DialogClose = DialogPrimitive.Close;

const DialogOverlay = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Overlay
    ref={ref}
    className={cn("fixed inset-0 z-40 bg-scrim", "data-[state=open]:animate-fade-in", className)}
    {...props}
  />
));
DialogOverlay.displayName = DialogPrimitive.Overlay.displayName;

interface DialogContentProps
  extends React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> {
  /** Accessible label for the close button (pass a translated string). */
  closeLabel: string;
}

const DialogContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  DialogContentProps
>(({ className, children, closeLabel, ...props }, ref) => (
  <DialogPortal>
    <DialogOverlay />
    <DialogPrimitive.Content
      ref={ref}
      className={cn(
        // Drawer (components-overlays.md): 400px, heavy inline-start rule, no
        // shadow - it would point off-screen. A raised scope for hover.
        "bx-raised fixed inset-y-0 end-0 z-40 flex w-full max-w-[400px] flex-col border-s border-line-heavy bg-popover text-popover-foreground",
        "data-[state=open]:animate-drawer-in rtl:data-[state=open]:animate-drawer-in-rtl",
        className,
      )}
      {...props}
    >
      {children}
      <DialogPrimitive.Close
        className="absolute end-2 top-2 inline-flex h-10 w-10 items-center justify-center text-ink-muted transition-colors duration-80 ease-linear hover:bg-surface-hover hover:text-ink"
        aria-label={closeLabel}
      >
        <XIcon className="h-5 w-5" aria-hidden="true" />
      </DialogPrimitive.Close>
    </DialogPrimitive.Content>
  </DialogPortal>
));
DialogContent.displayName = DialogPrimitive.Content.displayName;

const DialogTitle = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title
    ref={ref}
    className={cn("font-display text-md font-semibold text-foreground", className)}
    {...props}
  />
));
DialogTitle.displayName = DialogPrimitive.Title.displayName;

const DialogDescription = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Description
    ref={ref}
    className={cn("text-base text-muted-foreground", className)}
    {...props}
  />
));
DialogDescription.displayName = DialogPrimitive.Description.displayName;

export {
  Dialog,
  DialogTrigger,
  DialogPortal,
  DialogClose,
  DialogContent,
  DialogTitle,
  DialogDescription,
};
