/**
 * Spinner — Boxy's loading mark (components-data.md): a 4px accent square
 * stepping round the corners of a 16px ruled box. No arcs, no rotation.
 * In a button it replaces the icon slot, so the label does not reflow.
 */
import { cn } from "@/lib/utils";

interface SpinnerProps {
  /** 16px (default) or the 12px inline variant. */
  size?: "md" | "sm";
  className?: string;
}

export function Spinner({ size = "md", className }: SpinnerProps) {
  return (
    <span
      className={cn("bx-spinner", size === "sm" && "bx-spinner--sm", className)}
      aria-hidden="true"
      data-testid="spinner"
    />
  );
}
