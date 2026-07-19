/**
 * PluginsBadge — small status pill matching the plugin's row state.
 */
import { Badge } from "@/components/ui/badge";
import { useTranslation } from "react-i18next";

interface Props {
  status: "pending" | "active" | "disabled";
}

export function PluginStatusBadge({ status }: Props) {
  const { t } = useTranslation();
  const variant = status === "active" ? "default" : status === "pending" ? "secondary" : "outline";
  return <Badge variant={variant}>{t(`plugins.status.${status}`)}</Badge>;
}
