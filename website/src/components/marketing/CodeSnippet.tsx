/**
 * CodeSnippet — a copyable code block for the hero's "docker compose up"
 * command.
 *
 * Boxy code block: sunken 32px header strip with the language as a mono
 * label and a copy button at the inline end, 1px line, square, no
 * traffic-light dots. The command text is sourced from the locale bundle
 * (key `codeSnippet.command`). The block is always LTR, since shell commands
 * read left to right in every locale.
 */
import { useEffect, useRef, useState } from "react";
import { CheckIcon, CopyIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

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
    <div className={cn("w-full", className)}>
      <p className="bx-label mb-3">{t("codeSnippet.label")}</p>
      <div className="border border-line bg-surface">
        <div className="flex h-8 items-center justify-between border-b border-line bg-surface-sunken ps-3">
          <span className="bx-label">{t("codeSnippet.shell")}</span>
          <button
            type="button"
            onClick={onCopy}
            aria-label={t("codeSnippet.copyLabel")}
            className="inline-flex h-8 items-center gap-2 border-s border-line px-3 font-mono text-xs font-medium uppercase tracking-label text-ink-muted transition-colors duration-80 ease-linear hover:bg-surface-hover hover:text-ink"
          >
            {copied ? (
              <CheckIcon className="h-4 w-4" aria-hidden="true" />
            ) : (
              <CopyIcon className="h-4 w-4" aria-hidden="true" />
            )}
            <span aria-live="polite">
              {copied ? t("codeSnippet.copied") : t("codeSnippet.copy")}
            </span>
          </button>
        </div>
        <pre
          dir="ltr"
          className="overflow-x-auto p-4 text-start font-mono text-base text-foreground"
        >
          <code>
            <span className="select-none text-ink-subtle">{t("codeSnippet.comment")}</span>
            {"\n"}
            <span aria-hidden="true" className="select-none text-ink-faint before:content-['$_']" />
            <span className="font-medium">{t("codeSnippet.command")}</span>
          </code>
        </pre>
      </div>
    </div>
  );
}
