/**
 * Section — vertical-rhythm wrapper for marketing page sections.
 *
 * Boxy editorial recipe: a full-bleed 1px rule at the bottom of every
 * section (it runs past the content column to the viewport edges), 160px of
 * block padding on desktop (80px below md), and a railed container whose two
 * vertical hairlines line up from section to section.
 */
import { cn } from "@/lib/utils";

interface SectionProps extends React.HTMLAttributes<HTMLElement> {
  as?: "section" | "div" | "header" | "footer";
  containerClassName?: string;
  /** `tight` uses 128px instead of 160px, for page headers and short blocks. */
  rhythm?: "default" | "tight";
}

export function Section({
  as: Tag = "section",
  className,
  containerClassName,
  rhythm = "default",
  children,
  ...rest
}: SectionProps) {
  return (
    <Tag className={cn("border-b border-line", className)} {...rest}>
      <div
        className={cn(
          "site-rail",
          rhythm === "tight" ? "site-section--tight" : "site-section",
          containerClassName,
        )}
      >
        {children}
      </div>
    </Tag>
  );
}
