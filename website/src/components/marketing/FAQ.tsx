/**
 * FAQ — accordion-style FAQ using native <details>/<summary> for
 * accessibility (no JS required, keyboard-accessible out of the box).
 *
 * Boxy accordion (references/components-content.md): heading on the
 * inline-start side, numbered questions in one bordered container on the
 * other, a drawn plus/minus indicator, and `name="faq"` so opening one
 * closes the others natively.
 */
import { useTranslation } from "react-i18next";

import { Section } from "@/components/marketing/Section";
import { SectionHeading } from "@/components/marketing/SectionHeading";
import { useFormatNumber } from "@/lib/format";

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
  const fmt = useFormatNumber();

  return (
    <Section id="faq" containerClassName="grid grid-cols-1 gap-16 lg:grid-cols-12">
      <div className="lg:col-span-5">
        <SectionHeading
          eyebrow={t("faq.eyebrow")}
          title={t("faq.sectionTitle")}
          subtitle={t("faq.sectionSubtitle")}
        />
      </div>
      <div className="bx-accordion self-start lg:col-span-7">
        {FAQ_KEYS.map((key, i) => (
          <details key={key} name="faq">
            <summary className="text-md">
              <span className="bx-accordion__n" aria-hidden="true">
                {fmt(i + 1, 2)}
              </span>
              <span className="text-start">{t(`faq.items.${key}.q`)}</span>
            </summary>
            <div className="bx-accordion__body">
              <p className="max-w-[64ch]">{t(`faq.items.${key}.a`)}</p>
            </div>
          </details>
        ))}
      </div>
    </Section>
  );
}
