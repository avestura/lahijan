/**
 * cn — Tailwind-aware className combiner used by every shadcn primitive.
 *
 * Combines clsx (conditional class composition) with tailwind-merge (resolves
 * conflicting Tailwind utility classes by keeping the last one).
 */
import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
