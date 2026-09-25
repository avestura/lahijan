/**
 * DocsSidebar — the documentation tree (Boxy tree navigation,
 * references/components-navigation.md).
 *
 * Section headings are mono labels on sunken strips; groups are native
 * <details> toggles hanging off a literal tree rule; the current page is
 * marked with aria-current="page" and the accent bar. The group holding the
 * current page opens on navigation; other groups stay as the reader left them.
 */
import { Fragment, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";

import { DOCS_NAV, docsPath, isGroup } from "@/docs/nav";
import type { DocsGroup, DocsLink } from "@/docs/nav";

interface DocsSidebarProps {
  currentSlug: string | null;
  /** Called after a link is followed (closes the mobile drawer). */
  onNavigate?: () => void;
}

function TreeLink({ link, currentSlug, onNavigate }: { link: DocsLink } & DocsSidebarProps) {
  const current = link.slug === currentSlug;
  return (
    <li>
      <Link
        to={docsPath(link.slug)}
        className="bx-tree__item"
        aria-current={current ? "page" : undefined}
        onClick={onNavigate}
      >
        {link.title}
      </Link>
    </li>
  );
}

function TreeGroup({ group, currentSlug, onNavigate }: { group: DocsGroup } & DocsSidebarProps) {
  const containsCurrent = group.items.some((l) => l.slug === currentSlug);
  const [open, setOpen] = useState(containsCurrent);
  useEffect(() => {
    if (containsCurrent) setOpen(true);
  }, [containsCurrent]);

  return (
    <li>
      <details open={open} onToggle={(e) => setOpen(e.currentTarget.open)}>
        <summary className="bx-tree__item">{group.title}</summary>
        <ul>
          {group.items.map((link) => (
            <TreeLink
              key={link.slug}
              link={link}
              currentSlug={currentSlug}
              onNavigate={onNavigate}
            />
          ))}
        </ul>
      </details>
    </li>
  );
}

export function DocsSidebar({ currentSlug, onNavigate }: DocsSidebarProps) {
  const { t } = useTranslation();
  return (
    <nav className="bx-tree" aria-label={t("docs.navLabel")}>
      {DOCS_NAV.map((section) => (
        <Fragment key={section.title}>
          <p className="bx-tree__heading">{section.title}</p>
          <ul>
            {section.items.map((item) =>
              isGroup(item) ? (
                <TreeGroup
                  key={item.title}
                  group={item}
                  currentSlug={currentSlug}
                  onNavigate={onNavigate}
                />
              ) : (
                <TreeLink
                  key={item.slug}
                  link={item}
                  currentSlug={currentSlug}
                  onNavigate={onNavigate}
                />
              ),
            )}
          </ul>
        </Fragment>
      ))}
    </nav>
  );
}
