/**
 * FAQ — accordion-style FAQ using native <details>/<summary> for
 * accessibility (no JS required, keyboard-accessible out of the box).
 */
import { PlusIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Section } from "@/components/marketing/Section";
import { SectionHeading } from "@/components/marketing/SectionHeading";

type FaqKey = "selfhost" | "backends" | "multiTenant" | "pricing" | "plugins" | "sdk";

const FAQ_KEYS: readonly FaqKey[] = [
  "selfhost",
  "backends",
  "multiTenant",
  "pricing",
  "plugins",
  "sdk",
];

export function FAQ() {
  const { t } = useTranslation();

  return (
    <Section id="faq" className="bg-background">
      <SectionHeading title={t("faq.sectionTitle")} subtitle={t("faq.sectionSubtitle")} />
      <div className="mx-auto mt-12 max-w-3xl divide-y divide-border rounded-xl border border-border bg-card shadow-sm">
        {FAQ_KEYS.map((key) => (
          <details key={key} className="group p-5">
            <summary className="flex cursor-pointer list-none items-start justify-between gap-4 text-start text-base font-medium text-foreground">
              <span>{t(`faq.items.${key}.q`)}</span>
              <PlusIcon
                aria-hidden="true"
                className="mt-1 h-4 w-4 shrink-0 text-muted-foreground transition-transform group-open:rotate-45"
              />
            </summary>
            <p className="mt-3 text-sm leading-relaxed text-muted-foreground">
              {t(`faq.items.${key}.a`)}
            </p>
          </details>
        ))}
      </div>
    </Section>
  );
}
