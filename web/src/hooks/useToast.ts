/**
 * useToast — simple state-backed toast dispatcher.
 *
 * Mirrors the shadcn/ui reference implementation: a tiny in-memory queue
 * keyed by id; toasts auto-dismiss after the configured duration. Mount
 * `<Toaster />` once at the app root to render the queue.
 *
 * Why not a Zustand store? The queue is UI-only, ephemeral, and small.
 * A useState queue keeps the blast radius of a bug contained to the
 * toast subsystem.
 */
import * as React from "react";

import type { ToastActionElement, ToastProps } from "@/components/ui/toast";

const TOAST_LIMIT = 3;
const TOAST_DISMISS_MS = 5_000;

export type ToasterToast = ToastProps & {
  id: string;
  title?: React.ReactNode;
  description?: React.ReactNode;
  action?: ToastActionElement;
};

let count = 0;
function genId(): string {
  count = (count + 1) % Number.MAX_SAFE_INTEGER;
  return count.toString();
}

interface State {
  toasts: ToasterToast[];
}

const listeners: ((state: State) => void)[] = [];
let memoryState: State = { toasts: [] };

function emit(state: State): void {
  memoryState = state;
  for (const l of listeners) l(state);
}

function push(toast: ToasterToast): void {
  emit({
    // Cap the queue; keep the newest entries.
    toasts: [...memoryState.toasts, toast].slice(-TOAST_LIMIT),
  });
}

function dismiss(id: string): void {
  emit({
    toasts: memoryState.toasts.filter((t) => t.id !== id),
  });
}

export interface ToastOptions
  extends Omit<ToasterToast, "id" | "defaultOpen" | "onOpenChange" | "onEscapeKeyDown"> {
  /** Optional explicit dismiss override; default is 5s. */
  duration?: number;
}

function toast(opts: ToastOptions): {
  id: string;
  dismiss: () => void;
} {
  const id = genId();
  push({ ...opts, id, open: true, onOpenChange: (open) => !open && dismiss(id) });
  if (opts.duration !== Number.POSITIVE_INFINITY) {
    setTimeout(() => dismiss(id), opts.duration ?? TOAST_DISMISS_MS);
  }
  return { id, dismiss: () => dismiss(id) };
}

function useToast(): {
  toasts: ToasterToast[];
  toast: typeof toast;
  dismiss: typeof dismiss;
} {
  const [state, setState] = React.useState<State>(memoryState);

  React.useEffect(() => {
    listeners.push(setState);
    return () => {
      const idx = listeners.indexOf(setState);
      if (idx >= 0) listeners.splice(idx, 1);
    };
  }, []);

  return {
    toasts: state.toasts,
    toast,
    dismiss,
  };
}

export { useToast, toast, dismiss };
