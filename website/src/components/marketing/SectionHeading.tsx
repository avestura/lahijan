/**
 * SectionHeading — mono eyebrow + title + subtitle.
 *
 * Left-aligned by default (Boxy: text is left-aligned; centre only short
 * hero copy). The eyebrow is the mono uppercase label; hierarchy comes from
 * size and weight, never from colour.
 */
import { cn } from "@/lib/utils";

interface SectionHeadingProps {
  eyebrow?: string;
  title: string;
  subtitle?: string;
  align?: "center" | "start";
  /** Heading level. Page headers use h1, sections h2. */
  as?: "h1" | "h2";
  className?: string;
}

export function SectionHeading({
  eyebrow,
  title,
  subtitle,
  align = "start",
  as: Heading = "h2",
  className,
}: SectionHeadingProps) {
  return (
    <div className={cn(align === "center" ? "mx-auto text-center" : "text-start", className)}>
      {eyebrow ? <p className="bx-label">{eyebrow}</p> : null}
      <Heading
        className={cn(
          "max-w-[20ch] text-foreground",
          Heading === "h1" ? "site-subdisplay" : "text-3xl md:text-4xl",
          eyebrow && "mt-3",
          align === "center" && "mx-auto",
        )}
      >
        {title}
      </Heading>
      {subtitle ? (
        <p
          className={cn(
            "mt-5 max-w-[56ch] text-lg text-muted-foreground",
            align === "center" && "mx-auto",
          )}
        >
          {subtitle}
        </p>
      ) : null}
    </div>
  );
}
