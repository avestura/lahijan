/**
 * Docs entry page (`/docs`).
 *
 * The full docs site lives in `docs-site/` (Docusaurus, owned by WS-02).
 * This page is a soft entry that highlights the most-read doc sections and
 * links onward. The actual destination is configurable via env so each
 * deployer can point at their hosted docs.
 */
import { ArrowRightIcon, BookOpenIcon, CompassIcon, ServerIcon, TerminalIcon } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { CTASection } from "@/components/marketing/CTASection";
import { PageHeader } from "@/components/marketing/PageHeader";
import { Section } from "@/components/marketing/Section";
import { Button } from "@/components/ui/button";
import { SeoHead } from "@/components/seo/SeoHead";
import { DOCS_URL } from "@/lib/site";

interface DocLink {
  icon: LucideIcon;
  titleKey: string;
  bodyKey: string;
  anchor: string;
}

const DOC_LINKS: readonly DocLink[] = [
  {
    icon: TerminalIcon,
    titleKey: "page.docs.links.quickstart",
    bodyKey: "page.docs.links.quickstartBody",
    anchor: "quickstart",
  },
  {
    icon: CompassIcon,
    titleKey: "page.docs.links.concepts",
    bodyKey: "page.docs.links.conceptsBody",
    anchor: "concepts",
  },
  {
    icon: BookOpenIcon,
    titleKey: "page.docs.links.api",
    bodyKey: "page.docs.links.apiBody",
    anchor: "api",
  },
  {
    icon: ServerIcon,
    titleKey: "page.docs.links.operators",
    bodyKey: "page.docs.links.operatorsBody",
    anchor: "operators",
  },
];

export function DocsPage() {
  const { t } = useTranslation();

  return (
    <>
      <SeoHead titleKey="page.docs.title" descriptionKey="page.docs.lead" path="/docs" />
      <PageHeader
        eyebrow={t("page.docs.title")}
        title={t("page.docs.subtitle")}
        lead={t("page.docs.lead")}
      >
        <Button asChild size="lg" variant="outline" className="mt-10">
          <a href={DOCS_URL}>{t("page.docs.visitDocsSite")}</a>
        </Button>
      </PageHeader>

      <Section rhythm="tight">
        <ul
          role="list"
          className="site-bleed bx-collapse grid-cols-1 border-y border-line md:grid-cols-2"
        >
          {DOC_LINKS.map(({ icon: Icon, titleKey, bodyKey, anchor }) => (
            <li key={titleKey}>
              <a
                href={`${DOCS_URL}${anchor}`}
                className="group flex h-full items-start gap-6 px-4 py-10 text-foreground no-underline transition-colors duration-80 ease-linear hover:bg-surface-hover hover:no-underline md:px-8"
              >
                <span className="flex h-10 w-10 shrink-0 items-center justify-center border border-line bg-surface-sunken">
                  <Icon className="h-5 w-5" strokeWidth={1.5} aria-hidden="true" />
                </span>
                <span className="flex-1">
                  <span className="block font-display text-xl font-semibold tracking-heading">
                    {t(titleKey)}
                  </span>
                  <span className="mt-2 block text-base text-muted-foreground">{t(bodyKey)}</span>
                </span>
                <ArrowRightIcon
                  className="mt-1 h-5 w-5 shrink-0 text-ink-subtle group-hover:text-ink rtl:rotate-180"
                  aria-hidden="true"
                />
              </a>
            </li>
          ))}
        </ul>
      </Section>

      <CTASection compact />
    </>
  );
}
