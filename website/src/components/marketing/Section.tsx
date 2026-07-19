/**
 * Section — vertical-rhythm wrapper for marketing page sections.
 *
 * Use <Section> around every block on a landing page so spacing stays
 * consistent and RTL-safe.
 */
import { cn } from "@/lib/utils";

interface SectionProps extends React.HTMLAttributes<HTMLElement> {
  as?: "section" | "div" | "header" | "footer";
  containerClassName?: string;
}

export function Section({
  as: Tag = "section",
  className,
  containerClassName,
  children,
  ...rest
}: SectionProps) {
  return (
    <Tag className={cn("py-16 md:py-24", className)} {...rest}>
      <div className={cn("container", containerClassName)}>{children}</div>
    </Tag>
  );
}
