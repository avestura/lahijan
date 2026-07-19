/**
 * FeatureGrid — the six-pillar feature block used on the landing page and
 * mirrored on /features.
 *
 * Renders a feature card per pillar (compute, dns, storage, plugins, rbac,
 * billing). Each card is a semantic button-link to the matching anchored
 * section on /features (or, on /features itself, plain text cards).
 *
 * Icons come from lucide-react; the icon for each pillar is mapped in
 * FEATURE_DEFS below. Per pillar 1 (Transparent infrastructure) the cards
 * speak of "compute / DNS / object storage" — never the underlying backends'
 * names.
 */
import {
  CpuIcon,
  DatabaseIcon,
  KeyRoundIcon,
  NetworkIcon,
  PuzzleIcon,
  WalletIcon,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Section } from "@/components/marketing/Section";
import { SectionHeading } from "@/components/marketing/SectionHeading";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

type FeatureKey = "compute" | "dns" | "storage" | "plugins" | "rbac" | "billing";

interface FeatureDef {
  key: FeatureKey;
  icon: LucideIcon;
  anchor: string;
}

const FEATURE_DEFS: readonly FeatureDef[] = [
  { key: "compute", icon: CpuIcon, anchor: "#compute" },
  { key: "dns", icon: NetworkIcon, anchor: "#dns" },
  { key: "storage", icon: DatabaseIcon, anchor: "#storage" },
  { key: "plugins", icon: PuzzleIcon, anchor: "#plugins" },
  { key: "rbac", icon: KeyRoundIcon, anchor: "#rbac" },
  { key: "billing", icon: WalletIcon, anchor: "#billing" },
];

export function FeatureGrid() {
  const { t } = useTranslation();

  return (
    <Section id="features" className="bg-background">
      <SectionHeading title={t("features.sectionTitle")} subtitle={t("features.sectionSubtitle")} />
      <ul role="list" className="mt-12 grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
        {FEATURE_DEFS.map(({ key, icon: Icon, anchor }) => (
          <li key={key} id={key}>
            <Card className="h-full transition-shadow hover:shadow-lg">
              <a href={anchor} className="block h-full no-underline">
                <CardHeader>
                  <span className="mb-3 inline-flex h-10 w-10 items-center justify-center rounded-md bg-accent text-accent-foreground">
                    <Icon className="h-5 w-5" />
                  </span>
                  <CardTitle className="text-lg">{t(`features.${key}.title`)}</CardTitle>
                </CardHeader>
                <CardContent>
                  <CardDescription className="text-sm leading-relaxed">
                    {t(`features.${key}.body`)}
                  </CardDescription>
                </CardContent>
              </a>
            </Card>
          </li>
        ))}
      </ul>
    </Section>
  );
}
