/**
 * CheckboxGroup — pick any subset of a known list of options.
 *
 * Use this instead of a comma-separated text input whenever the possible
 * values are known up front (tool names, permission scopes, roles, ...).
 * Values already selected but missing from `options` (for example a legacy
 * value saved by an older release) are still shown and stay selected, so
 * saving the form never silently drops data.
 */
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

export interface CheckboxGroupOption {
  /** Stored value (what ends up in the array). */
  value: string;
  /** Human label, already translated. */
  label: string;
  /** Optional secondary line, already translated. */
  description?: string;
  /** Optional heading the option is listed under, already translated. */
  group?: string;
}

interface CheckboxGroupProps {
  /** Unique id prefix for the checkbox inputs. */
  id: string;
  options: readonly CheckboxGroupOption[];
  value: readonly string[];
  onChange: (next: string[]) => void;
  disabled?: boolean;
  /** Hide the "select all / clear" buttons. */
  hideBulkActions?: boolean;
  className?: string;
}

export function CheckboxGroup({
  id,
  options,
  value,
  onChange,
  disabled,
  hideBulkActions,
  className,
}: CheckboxGroupProps) {
  const { t } = useTranslation();

  const known = new Set(options.map((o) => o.value));
  const extras: CheckboxGroupOption[] = value
    .filter((v) => !known.has(v))
    .map((v) => ({ value: v, label: v }));
  const all = [...options, ...extras];

  const selected = new Set(value);
  const toggle = (v: string, checked: boolean) => {
    const next = new Set(selected);
    if (checked) next.add(v);
    else next.delete(v);
    // Keep the option order stable so saved arrays do not reshuffle.
    onChange(all.map((o) => o.value).filter((x) => next.has(x)));
  };

  // Group while preserving first-seen order; ungrouped options come first.
  const groups: { heading?: string; items: CheckboxGroupOption[] }[] = [];
  for (const o of all) {
    let g = groups.find((x) => x.heading === o.group);
    if (!g) {
      g = { heading: o.group, items: [] };
      groups.push(g);
    }
    g.items.push(o);
  }

  return (
    <div className={cn("space-y-3", className)}>
      {!hideBulkActions && all.length > 1 && (
        <div className="flex gap-2">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={disabled}
            onClick={() => onChange(all.map((o) => o.value))}
          >
            {t("common.selectAll")}
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            disabled={disabled === true || value.length === 0}
            onClick={() => onChange([])}
          >
            {t("common.clearAll")}
          </Button>
        </div>
      )}
      {groups.map((g) => (
        <fieldset key={g.heading ?? "_"} className="space-y-2">
          {g.heading && (
            <legend className="mb-1 text-xs font-medium uppercase tracking-wider text-muted-foreground">
              {g.heading}
            </legend>
          )}
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            {g.items.map((o) => {
              const cid = `${id}-${o.value}`;
              return (
                <div key={o.value} className="flex items-start gap-2">
                  <Checkbox
                    id={cid}
                    className="mt-0.5"
                    checked={selected.has(o.value)}
                    disabled={disabled}
                    onCheckedChange={(c) => toggle(o.value, c === true)}
                  />
                  <div className="space-y-0.5">
                    <Label htmlFor={cid} className="cursor-pointer">
                      {o.label}
                    </Label>
                    {o.description && (
                      <p className="text-xs text-muted-foreground">{o.description}</p>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        </fieldset>
      ))}
    </div>
  );
}
