/**
 * InstanceConsole — xterm.js-backed command runner.
 *
 * The backend (WS-14) ships a one-shot `POST /instances/{id}/exec`
 * endpoint that runs an argv and returns captured stdout/stderr/exit.
 * The interactive bidirectional xterm.js console (true shell session)
 * lands later — it requires a websocket bridge the Incus provider
 * doesn't expose yet.
 *
 * For now, this panel:
 *   - renders an xterm.js terminal for output (true ANSI color support,
 *     selectable text, etc.)
 *   - prompts the user for a command via an input box
 *   - prints the captured output + exit code
 *
 * When the interactive bridge lands, this component is the natural
 * place to extend; the xterm.js setup is already done here.
 */
import "@xterm/xterm/css/xterm.css";

import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useExecInstance, classifyStatus } from "../api";

interface Props {
  instanceId: string;
  /** When the instance is stopped we surface a "disabled" hint. */
  status: string | undefined;
}

export function InstanceConsole({ instanceId, status }: Props) {
  const { t } = useTranslation();
  const [command, setCommand] = useState("");
  const exec = useExecInstance(instanceId);
  const termRef = useRef<HTMLDivElement | null>(null);
  const term = useRef<Terminal | null>(null);
  const fit = useRef<FitAddon | null>(null);

  // Boot the terminal once.
  useEffect(() => {
    if (!termRef.current) return;
    const termInst = new Terminal({
      fontFamily: "var(--font-mono), ui-monospace, monospace",
      fontSize: 13,
      convertEol: true,
      cursorBlink: false,
      disableStdin: true,
    });
    const fitAddon = new FitAddon();
    termInst.loadAddon(fitAddon);
    termInst.open(termRef.current);
    fitAddon.fit();
    termInst.writeln(t("compute.console.placeholder"));
    term.current = termInst;
    fit.current = fitAddon;

    // Re-fit on window resize.
    const onResize = () => fit.current?.fit();
    window.addEventListener("resize", onResize);

    return () => {
      window.removeEventListener("resize", onResize);
      termInst.dispose();
      term.current = null;
      fit.current = null;
    };
    // We intentionally only run this once on mount; the welcome line is
    // static. The lint rule's exhaustive-deps expectation doesn't add
    // safety here.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Each run pushes output into the same terminal.
  useEffect(() => {
    if (!term.current) return;
    if (exec.isIdle) return;
    if (exec.isError) {
      term.current.writeln(`\x1b[31m${t("common.error")}\x1b[0m`);
      return;
    }
    if (exec.data) {
      term.current.writeln(`\x1b[2m$ ${command}\x1b[0m`);
      if (exec.data.stdout) term.current.writeln(exec.data.stdout);
      if (exec.data.stderr) term.current.writeln(`\x1b[33m${exec.data.stderr}\x1b[0m`);
      term.current.writeln(t("compute.console.exitCode", { code: exec.data.exitCode }));
    }
    // We only want to print on data transitions; command is captured in
    // the closure for the banner line.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [exec.data, exec.isError, exec.isIdle]);

  const onRun = (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = command.trim();
    if (!trimmed) return;
    // Split on whitespace for the argv. Quote handling is intentionally
    // minimal — the backend's exec takes the argv verbatim, and this is
    // the simple-path MVP. A richer parser can land later.
    const argv = trimmed.split(/\s+/);
    exec.mutate({ command: argv });
    setCommand("");
  };

  const bucket = classifyStatus(status);
  const disabled = bucket !== "running";

  return (
    <div className="space-y-3">
      <form onSubmit={onRun} className="flex items-center gap-2">
        <Input
          value={command}
          onChange={(e) => setCommand(e.target.value)}
          placeholder={t("compute.console.placeholder")}
          disabled={disabled || exec.isPending}
          aria-label={t("compute.console.title")}
        />
        <Button type="submit" disabled={disabled || exec.isPending || !command.trim()}>
          {exec.isPending ? t("compute.console.running") : t("compute.console.run")}
        </Button>
      </form>
      {disabled && <p className="text-xs text-muted-foreground">{t("compute.console.disconnected")}</p>}
      <p className="text-xs text-muted-foreground">{t("compute.console.hint")}</p>
      <div
        ref={termRef}
        className="h-72 overflow-hidden rounded-md border border-border bg-black p-2"
        aria-label={t("compute.console.title")}
      />
    </div>
  );
}
