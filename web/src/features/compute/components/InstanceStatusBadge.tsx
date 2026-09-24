/**
 * InstanceStatusBadge — an instance's state as an 8px square plus a label
 * (Boxy industrial: status is a square, never a circle, and color is never
 * the only signal — the text label always accompanies it).
 *
 * The compute backend emits a free-form status string ("Running",
 * "Stopped", "Frozen", "Starting", ...). We bucket it into the four
 * categories the UI filter exposes and pick a tone for each.
 */
import { useTranslation } from "react-i18next";

import { cn } from "@/lib/utils";
import { classifyStatus, type StatusBucket } from "../api";

type Tone = "success" | "muted" | "neutral" | "warning";

const TONE_BY_BUCKET: Record<StatusBucket, Tone> = {
  running: "success",
  stopped: "muted",
  frozen: "neutral",
  other: "warning",
};

const SQUARE_BY_TONE: Record<Tone, string> = {
  success: "bg-success",
  muted: "bg-ink-faint",
  neutral: "bg-surface-inverse",
  warning: "bg-warning",
};

interface Props {
  status: string | undefined;
}

export function InstanceStatusBadge({ status }: Props) {
  const { t } = useTranslation();
  const bucket = classifyStatus(status);
  const tone = TONE_BY_BUCKET[bucket];
  const labelKey = bucket === "other" ? "compute.status.other" : `compute.status.${bucket}`;
  return (
    <span
      className="inline-flex items-center gap-2 whitespace-nowrap font-mono text-label font-medium uppercase tracking-[0.08em] text-ink-muted"
      data-tone={tone}
      aria-live="polite"
    >
      <span aria-hidden="true" className={cn("h-2 w-2 shrink-0", SQUARE_BY_TONE[tone])} />
      {t(labelKey)}
    </span>
  );
}
