/**
 * DocsSection — the landing page's way into the documentation.
 *
 * Editorial section with a heading and a link to the docs home, then the
 * four most-used entry points as a collapsed grid of link cells (the Boxy
 * card-link treatment: the whole cell is the hit area, hover fills it, an
 * arrow sits at the inline end).
 */
import {
  ArrowRightIcon,
  BookOpenIcon,
  ServerIcon,
  SlidersHorizontalIcon,
  TerminalIcon,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { Section } from "@/components/marketing/Section";
import { SectionHeading } from "@/components/marketing/SectionHeading";
import { Button } from "@/components/ui/button";
import { DOCS_PATH } from "@/lib/site";

interface Entry {
  icon: LucideIcon;
  key: string;
  to: string;
}

const ENTRIES: readonly Entry[] = [
  { icon: TerminalIcon, key: "quickstart", to: "/docs/getting-started/quickstart" },
  { icon: ServerIcon, key: "install", to: "/docs/getting-started/installation" },
  { icon: SlidersHorizontalIcon, key: "configuration", to: "/docs/reference/configuration" },
  { icon: BookOpenIcon, key: "api", to: "/docs/reference/api" },
];

export function DocsSection() {
  const { t } = useTranslation();
  return (
    <Section id="docs">
      <div className="flex flex-col items-start justify-between gap-8 md:flex-row md:items-end">
        <SectionHeading
          eyebrow={t("docsSection.eyebrow")}
          title={t("docsSection.title")}
          subtitle={t("docsSection.subtitle")}
        />
        <Button asChild size="lg" variant="outline" className="shrink-0">
          <Link to={DOCS_PATH}>{t("cta.readDocs")}</Link>
        </Button>
      </div>
      <ul
        role="list"
        className="site-bleed bx-collapse mt-16 grid-cols-1 border-y border-line md:grid-cols-2 lg:grid-cols-4"
      >
        {ENTRIES.map(({ icon: Icon, key, to }) => (
          <li key={key}>
            <Link
              to={to}
              className="group flex h-full flex-col gap-4 px-4 py-10 text-foreground no-underline transition-colors duration-80 ease-linear hover:bg-surface-hover hover:no-underline md:px-8"
            >
              <span className="flex items-center justify-between">
                <Icon className="h-6 w-6 text-ink-subtle" aria-hidden="true" />
                <ArrowRightIcon
                  className="h-4 w-4 text-ink-subtle transition-transform duration-80 ease-linear group-hover:translate-x-1 rtl:rotate-180 rtl:group-hover:-translate-x-1"
                  aria-hidden="true"
                />
              </span>
              <span className="text-xl">{t(`docsSection.${key}.title`)}</span>
              <span className="max-w-[40ch] text-base text-muted-foreground">
                {t(`docsSection.${key}.body`)}
              </span>
            </Link>
          </li>
        ))}
      </ul>
    </Section>
  );
}
