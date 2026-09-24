/**
 * Testimonials — placeholder quote cells.
 *
 * Per WS-19 open question 1 / "testimonial placeholder section (filled by
 * operator)", these are stock placeholder quotes that an operator customizes
 * in their own deployment (via the locale bundle).
 *
 * Boxy: a collapsed grid of figures; each attribution has a square avatar
 * (the initial on a sunken tile with a 1px line), never a circle.
 */
import { useTranslation } from "react-i18next";

import { Section } from "@/components/marketing/Section";
import { SectionHeading } from "@/components/marketing/SectionHeading";

type TKey = "t1" | "t2" | "t3";
const T_KEYS: readonly TKey[] = ["t1", "t2", "t3"];

export function Testimonials() {
  const { t } = useTranslation();
  return (
    <Section id="testimonials">
      <SectionHeading
        eyebrow={t("testimonials.eyebrow")}
        title={t("testimonials.sectionTitle")}
        subtitle={t("testimonials.sectionSubtitle")}
      />
      <ul
        role="list"
        className="site-bleed bx-collapse mt-16 grid-cols-1 border-y border-line md:grid-cols-3"
      >
        {T_KEYS.map((key) => {
          const name = t(`testimonials.items.${key}.name`);
          return (
            <li key={key}>
              <figure className="flex h-full flex-col px-4 py-10 md:px-8">
                <blockquote className="font-display text-xl font-medium tracking-heading text-foreground">
                  {t(`testimonials.items.${key}.quote`)}
                </blockquote>
                <figcaption className="mt-auto flex items-center gap-3 pt-10">
                  <span
                    aria-hidden="true"
                    className="flex h-10 w-10 shrink-0 items-center justify-center border border-line bg-surface-sunken font-mono text-base font-medium text-ink"
                  >
                    {name.charAt(0)}
                  </span>
                  <span className="text-base">
                    <span className="block font-semibold text-foreground">{name}</span>
                    <span className="block text-ink-subtle">
                      {t(`testimonials.items.${key}.role`)}
                    </span>
                  </span>
                </figcaption>
              </figure>
            </li>
          );
        })}
      </ul>
    </Section>
  );
}
