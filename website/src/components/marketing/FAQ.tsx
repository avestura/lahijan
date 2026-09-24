/**
 * FAQ — accordion-style FAQ using native <details>/<summary> for
 * accessibility (no JS required, keyboard-accessible out of the box).
 *
 * Boxy: heading on the inline-start side, questions as full-width ruled rows
 * on the other. The plus/minus glyph swaps state instantly; nothing rotates.
 */
import { MinusIcon, PlusIcon } from "lucide-react";
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
    <Section id="faq" containerClassName="grid grid-cols-1 gap-16 lg:grid-cols-12">
      <div className="lg:col-span-5">
        <SectionHeading
          eyebrow={t("faq.eyebrow")}
          title={t("faq.sectionTitle")}
          subtitle={t("faq.sectionSubtitle")}
        />
      </div>
      <div className="border-t border-line lg:col-span-7">
        {FAQ_KEYS.map((key) => (
          <details key={key} className="group border-b border-line">
            <summary className="flex cursor-pointer list-none items-start justify-between gap-6 py-6 text-start font-display text-lg font-medium text-foreground transition-colors duration-80 ease-linear hover:text-ink-muted [&::-webkit-details-marker]:hidden">
              <span>{t(`faq.items.${key}.q`)}</span>
              <PlusIcon
                aria-hidden="true"
                className="mt-1 h-5 w-5 shrink-0 text-ink-subtle group-open:hidden"
              />
              <MinusIcon
                aria-hidden="true"
                className="mt-1 hidden h-5 w-5 shrink-0 text-ink-subtle group-open:block"
              />
            </summary>
            <p className="max-w-[64ch] pb-6 text-md text-muted-foreground">
              {t(`faq.items.${key}.a`)}
            </p>
          </details>
        ))}
      </div>
    </Section>
  );
}
