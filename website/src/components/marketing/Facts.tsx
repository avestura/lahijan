/**
 * Facts — a collapsed row of metric tiles directly under the hero.
 *
 * Mono tabular figures with a muted caption, cells sharing 1px rules and
 * running rail to rail. Figures are formatted for the active locale.
 */
import { useTranslation } from "react-i18next";

import { useFormatNumber } from "@/lib/format";

const FACTS = [
  { value: 1, key: "facts.binary" },
  { value: 3, key: "facts.services" },
  { value: 60, key: "facts.metering" },
  { value: 2, key: "facts.locales" },
] as const;

export function Facts() {
  const { t } = useTranslation();
  const fmt = useFormatNumber();

  return (
    <section className="border-b border-line" aria-label={t("facts.label")}>
      <div className="site-rail">
        <dl className="site-bleed bx-collapse grid-cols-2 lg:grid-cols-4">
          {FACTS.map(({ value, key }) => (
            <div key={key} className="flex flex-col gap-3 px-4 py-8 md:px-8 md:py-10">
              <dt className="order-2 max-w-[28ch] text-base text-muted-foreground">{t(key)}</dt>
              <dd className="bx-mono order-1 text-4xl font-medium text-foreground md:text-5xl">
                {fmt(value)}
              </dd>
            </div>
          ))}
        </dl>
      </div>
    </section>
  );
}
