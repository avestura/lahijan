/**
 * PageHeader — the stacked editorial header for inner pages.
 *
 * Mono eyebrow (the page name), a display-size h1 capped at ~16ch, and a
 * lead paragraph at 18px capped at 56ch. Left-aligned, hanging off the rail.
 */
import { Section } from "@/components/marketing/Section";

interface PageHeaderProps {
  eyebrow: string;
  title: string;
  lead?: string;
  children?: React.ReactNode;
}

export function PageHeader({ eyebrow, title, lead, children }: PageHeaderProps) {
  return (
    <Section as="header" rhythm="tight">
      <p className="bx-label">{eyebrow}</p>
      <h1 className="site-display mt-6 max-w-[16ch] text-foreground">{title}</h1>
      {lead ? <p className="mt-6 max-w-[56ch] text-lg text-muted-foreground">{lead}</p> : null}
      {children}
    </Section>
  );
}
