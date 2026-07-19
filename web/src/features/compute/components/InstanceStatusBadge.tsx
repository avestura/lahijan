/**
 * InstanceStatusBadge — colored pill summarizing an instance's Incus
 * status string.
 *
 * The Incus daemon emits a free-form status string ("Running",
 * "Stopped", "Frozen", "Starting", ...). We bucket it into the four
 * categories the UI filter exposes and pick a tone that matches the
 * design system.
 */
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { useTranslation } from "react-i18next";
import { classifyStatus, type StatusBucket } from "../api";

const TONE_BY_BUCKET: Record<StatusBucket, BadgeProps["variant"]> = {
  running: "success",
  stopped: "muted",
  frozen: "secondary",
  other: "warning",
};

interface Props {
  status: string | undefined;
}

export function InstanceStatusBadge({ status }: Props) {
  const { t } = useTranslation();
  const bucket = classifyStatus(status);
  const labelKey = bucket === "other" ? "compute.status.other" : `compute.status.${bucket}`;
  return (
    <Badge variant={TONE_BY_BUCKET[bucket]} aria-live="polite">
      {t(labelKey)}
    </Badge>
  );
}
