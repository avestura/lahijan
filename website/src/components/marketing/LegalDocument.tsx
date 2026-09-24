/**
 * LegalDocument — the shared layout for /legal/privacy and /legal/terms.
 *
 * Page header with the "last updated" line as a mono label, then numbered
 * sections as ruled rows: mono number + title on the inline-start side,
 * body capped at the prose measure on the other.
 */
import { useTranslation } from "react-i18next";

import { PageHeader } from "@/components/marketing/PageHeader";
import { Section } from "@/components/marketing/Section";
import { useFormatNumber } from "@/lib/format";

interface LegalDocumentProps {
  /** i18n prefix, e.g. `page.legal.privacy`. */
  prefix: string;
  sections: readonly string[];
}

export function LegalDocument({ prefix, sections }: LegalDocumentProps) {
  const { t } = useTranslation();
  const fmt = useFormatNumber();
  const updated = new Date().toISOString().slice(0, 10);

  return (
    <>
      <PageHeader
        eyebrow={t("footer.legal")}
        title={t(`${prefix}.title`)}
        lead={t(`${prefix}.lead`)}
      >
        <p className="bx-label mt-8">
          {t(`${prefix}.updated`)}: <span dir="ltr">{updated}</span>
        </p>
      </PageHeader>
      <Section rhythm="tight">
        <article className="site-bleed border-t border-line">
          {sections.map((section, idx) => (
            <section
              key={section}
              className="grid grid-cols-1 gap-4 border-b border-line px-4 py-10 md:px-8 lg:grid-cols-12"
            >
              <div className="lg:col-span-4">
                <p className="bx-label">{fmt(idx + 1, 2)}</p>
                <h2 className="mt-2 text-xl">{t(`${prefix}.sections.${section}.title`)}</h2>
              </div>
              <p className="max-w-prose text-md text-muted-foreground lg:col-span-8">
                {t(`${prefix}.sections.${section}.body`)}
              </p>
            </section>
          ))}
        </article>
      </Section>
    </>
  );
}
