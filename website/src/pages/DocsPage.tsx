/**
 * Docs entry page (`/docs`).
 *
 * The full docs site lives in `docs-site/` (Docusaurus, owned by WS-02).
 * This page is a soft entry that highlights the most-read doc sections and
 * links onward. The actual destination is configurable via env so each
 * deployer can point at their hosted docs.
 */
import { BookOpenIcon, CompassIcon, ServerIcon, TerminalIcon } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { CTASection } from "@/components/marketing/CTASection";
import { Section } from "@/components/marketing/Section";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { SeoHead } from "@/components/seo/SeoHead";
import { absoluteUrl } from "@/lib/site";

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
    anchor: "/docs/quickstart",
  },
  {
    icon: CompassIcon,
    titleKey: "page.docs.links.concepts",
    bodyKey: "page.docs.links.conceptsBody",
    anchor: "/docs/concepts",
  },
  {
    icon: BookOpenIcon,
    titleKey: "page.docs.links.api",
    bodyKey: "page.docs.links.apiBody",
    anchor: "/docs/api",
  },
  {
    icon: ServerIcon,
    titleKey: "page.docs.links.operators",
    bodyKey: "page.docs.links.operatorsBody",
    anchor: "/docs/operators",
  },
];

/**
 * Canonical docs site URL. Defaults to a sibling /docs-site deploy; deployers
 * override by setting VITE_DOCS_URL or by editing this default.
 */
const DOCS_URL = absoluteUrl("/docs/");

export function DocsPage() {
  const { t } = useTranslation();

  return (
    <>
      <SeoHead titleKey="page.docs.title" descriptionKey="page.docs.lead" path="/docs" />
      <Section className="bg-background">
        <div className="mx-auto max-w-3xl text-center">
          <h1 className="text-4xl font-extrabold tracking-tight text-foreground md:text-5xl">
            {t("page.docs.title")}
          </h1>
          <p className="mt-4 text-lg text-muted-foreground">{t("page.docs.subtitle")}</p>
          <p className="mt-6 text-base text-muted-foreground">{t("page.docs.lead")}</p>
        </div>
      </Section>

      <Section className="bg-muted/30">
        <ul role="list" className="mx-auto grid max-w-4xl grid-cols-1 gap-6 md:grid-cols-2">
          {DOC_LINKS.map(({ icon: Icon, titleKey, bodyKey, anchor }) => (
            <li key={titleKey}>
              <a href={`${DOCS_URL}${anchor}`} className="block h-full no-underline">
                <Card className="h-full transition-shadow hover:shadow-lg">
                  <CardHeader>
                    <span className="mb-3 inline-flex h-10 w-10 items-center justify-center rounded-md bg-accent text-accent-foreground">
                      <Icon className="h-5 w-5" />
                    </span>
                    <CardTitle className="text-base">{t(titleKey)}</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <p className="text-sm leading-relaxed text-muted-foreground">{t(bodyKey)}</p>
                  </CardContent>
                </Card>
              </a>
            </li>
          ))}
        </ul>
        <div className="mt-10 text-center">
          <Button asChild size="lg" variant="outline">
            <a href={DOCS_URL}>{t("page.docs.visitDocsSite")}</a>
          </Button>
        </div>
      </Section>

      <CTASection compact />
    </>
  );
}
