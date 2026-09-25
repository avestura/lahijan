/**
 * Hero — top-of-page hero block for the landing page.
 *
 * Boxy editorial split hero: 7/5 columns with a 1px vertical rule between
 * them. Copy on the inline-start side (eyebrow tag, display h1 capped at
 * 14ch, lead, a shared-border action group with the page's single primary
 * action, and a mono trust line). The product surface on the inline-end side
 * runs out to the rail: the service matrix end users get, then the one-line
 * install command.
 *
 * Per pillar 1 the services are "compute / DNS / object storage", never the
 * underlying backends' names.
 */
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { CodeSnippet } from "@/components/marketing/CodeSnippet";
import { Button, ButtonGroup } from "@/components/ui/button";
import { useFormatNumber } from "@/lib/format";
import { DOCS_PATH, INSTALL_PATH } from "@/lib/site";

const STACK = [
  { titleKey: "features.compute.title", bodyKey: "hero.stack.compute" },
  { titleKey: "features.dns.title", bodyKey: "hero.stack.dns" },
  { titleKey: "features.storage.title", bodyKey: "hero.stack.storage" },
  { titleKey: "hero.stack.extensionsTitle", bodyKey: "hero.stack.extensions" },
] as const;

export function Hero() {
  const { t } = useTranslation();
  const fmt = useFormatNumber();

  return (
    <section className="border-b border-line">
      <div className="site-rail">
        <div className="site-bleed grid grid-cols-1 lg:grid-cols-12">
          <div className="px-4 py-20 md:px-8 md:py-32 lg:col-span-7 lg:pe-16">
            <span className="inline-flex h-6 items-center gap-2 border border-line bg-surface-sunken px-2 font-mono text-xs font-medium uppercase tracking-label text-ink-muted">
              <span className="site-status" aria-hidden="true" />
              {t("hero.badge")}
            </span>

            <h1 className="site-display mt-6 max-w-[14ch] text-foreground">{t("hero.title")}</h1>

            <p className="mt-6 max-w-[52ch] text-lg text-muted-foreground">{t("hero.subtitle")}</p>

            <ButtonGroup className="mt-10">
              <Button asChild size="lg">
                <Link to={INSTALL_PATH}>{t("hero.primary")}</Link>
              </Button>
              <Button asChild size="lg" variant="outline">
                <Link to={DOCS_PATH}>{t("cta.readDocs")}</Link>
              </Button>
            </ButtonGroup>

            <p className="bx-label mt-6">{t("hero.trust")}</p>
          </div>

          <div className="flex flex-col border-t border-line bg-surface-sunken lg:col-span-5 lg:border-s lg:border-t-0">
            <p className="bx-label flex h-10 items-center border-b border-line px-6">
              {t("hero.stack.label")}
            </p>
            <dl>
              {STACK.map(({ titleKey, bodyKey }, idx) => (
                <div
                  key={bodyKey}
                  className="grid grid-cols-[1fr_auto] gap-x-4 border-b border-line bg-surface px-6 py-5"
                >
                  <dt className="font-display text-md font-semibold text-foreground">
                    {t(titleKey)}
                  </dt>
                  <dd className="bx-mono row-span-2 text-xs text-ink-subtle" aria-hidden="true">
                    {fmt(idx + 1, 2)}
                  </dd>
                  <dd className="mt-1 text-base text-muted-foreground">{t(bodyKey)}</dd>
                </div>
              ))}
            </dl>
            <div className="mt-auto p-6">
              <CodeSnippet />
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
