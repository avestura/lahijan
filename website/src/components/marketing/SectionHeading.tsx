/**
 * SectionHeading — eyebrow + title + subtitle, centered by default.
 *
 * Used at the top of every marketing section to keep the visual rhythm.
 */
import { cn } from "@/lib/utils";

interface SectionHeadingProps {
  eyebrow?: string;
  title: string;
  subtitle?: string;
  align?: "center" | "start";
  className?: string;
}

export function SectionHeading({
  eyebrow,
  title,
  subtitle,
  align = "center",
  className,
}: SectionHeadingProps) {
  return (
    <div
      className={cn(
        "max-w-3xl",
        align === "center" ? "mx-auto text-center" : "text-start",
        className,
      )}
    >
      {eyebrow ? (
        <p className="mb-3 text-sm font-semibold uppercase tracking-wide text-primary">{eyebrow}</p>
      ) : null}
      <h2 className="text-3xl font-bold tracking-tight text-foreground md:text-4xl">{title}</h2>
      {subtitle ? <p className="mt-4 text-lg text-muted-foreground">{subtitle}</p> : null}
    </div>
  );
}
