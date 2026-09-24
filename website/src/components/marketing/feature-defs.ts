/**
 * The platform's feature list, shared by FeatureGrid (landing + /features)
 * and the /features detail rows. Order is the reading order; `span` keeps
 * every row of the collapsed grid full at md (2 cols) and lg (3 cols).
 */
import {
  BotIcon,
  CpuIcon,
  DatabaseIcon,
  KeyRoundIcon,
  NetworkIcon,
  PuzzleIcon,
  WalletIcon,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

export type FeatureKey = "compute" | "dns" | "storage" | "plugins" | "rbac" | "billing" | "agent";

export interface FeatureDef {
  key: FeatureKey;
  icon: LucideIcon;
  span?: string;
}

export const FEATURE_DEFS: readonly FeatureDef[] = [
  { key: "compute", icon: CpuIcon, span: "md:col-span-2" },
  { key: "dns", icon: NetworkIcon },
  { key: "storage", icon: DatabaseIcon },
  { key: "plugins", icon: PuzzleIcon },
  { key: "rbac", icon: KeyRoundIcon },
  { key: "billing", icon: WalletIcon },
  { key: "agent", icon: BotIcon, span: "lg:col-span-2" },
];
