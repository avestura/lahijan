/**
 * CodeSnippet — a styled, copyable code block for the hero's
 * "docker compose up" command.
 *
 * The actual command text is sourced from the locale bundle (key
 * `codeSnippet.command`) so a deployer can localize it if needed. The
 * clipboard button uses the Clipboard API; on copy it shows a transient
 * "Copied" label that auto-clears.
 */
import { useEffect, useRef, useState } from "react";
import { CheckIcon, ClipboardCopyIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export function CodeSnippet({ className }: { className?: string }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (timer.current) clearTimeout(timer.current);
    };
  }, []);

  async function onCopy() {
    const text = t("codeSnippet.command");
    try {
      if (typeof navigator !== "undefined" && navigator.clipboard) {
        await navigator.clipboard.writeText(text);
      }
      setCopied(true);
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard might be unavailable (e.g. older browsers, non-secure
      // contexts). Fail silently; the user can still select+copy manually.
    }
  }

  return (
    <div className={cn("mx-auto w-full max-w-2xl", className)}>
      <p className="mb-2 text-center text-xs font-medium uppercase tracking-wide text-muted-foreground">
        {t("codeSnippet.label")}
      </p>
      <div className="relative overflow-hidden rounded-lg border border-border bg-card shadow-lg">
        <div className="flex items-center justify-between border-b border-border bg-muted/50 px-4 py-2">
          <div className="flex items-center gap-2">
            <span className="h-3 w-3 rounded-full bg-destructive/70" />
            <span className="h-3 w-3 rounded-full bg-warning/70" />
            <span className="h-3 w-3 rounded-full bg-success/70" />
          </div>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={onCopy}
            aria-label={t("codeSnippet.copied")}
            className="h-7 gap-1 px-2 text-xs text-muted-foreground"
          >
            {copied ? (
              <>
                <CheckIcon className="h-3.5 w-3.5" />
                {t("codeSnippet.copied")}
              </>
            ) : (
              <>
                <ClipboardCopyIcon className="h-3.5 w-3.5" />
                {t("codeSnippet.copied")}
              </>
            )}
          </Button>
        </div>
        <pre className="overflow-x-auto p-4 text-start font-mono text-sm leading-relaxed text-foreground">
          <code>
            <span className="select-none text-muted-foreground">{t("codeSnippet.comment")}</span>
            {"\n"}
            <span className="font-semibold text-primary">{t("codeSnippet.command")}</span>
          </code>
        </pre>
      </div>
    </div>
  );
}
