/**
 * cn — Tailwind-aware className combiner used by every shadcn primitive.
 *
 * Combines clsx (conditional class composition) with tailwind-merge (resolves
 * conflicting Tailwind utility classes by keeping the last one). The
 * combination is what shadcn/ui's docs prescribe.
 *
 * tailwind-merge is taught the project's custom scales (tailwind.config.ts):
 * without it, `text-label` / `text-2xs` look like text *colors* and get
 * dropped whenever a real color such as `text-ink-subtle` follows, and the
 * hard-offset `shadow-hard-*` names would not dedupe against each other.
 */
import { clsx, type ClassValue } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

const twMerge = extendTailwindMerge({
  extend: {
    classGroups: {
      "font-size": [{ text: ["2xs", "label"] }],
      shadow: [{ shadow: ["seam", "hard-2", "hard-3", "ring"] }],
    },
  },
});

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
