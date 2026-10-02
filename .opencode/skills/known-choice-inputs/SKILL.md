---
name: known-choice-inputs
description: "Use when building or changing any form field where the user picks values from a set that is known up front: permissions or token scopes, tool names, roles, resource types, units, providers, regions, locales, statuses. Triggers on comma-separated text inputs, 'enter slugs separated by commas', splitCsv, free-text fields for enum-like values, <Input> used for a known list. Enforces checkboxes (many) or a select (one) instead of typing."
---

# Known-choice inputs — pick from a list, don't type a list

If the set of valid values is known when the form is built, the user must
**choose** values, never **type** them. Typing means typos, guessing what is
allowed, and finding out only after the server rejects it.

| The user picks…                       | Use                                    | Never                          |
| ------------------------------------- | -------------------------------------- | ------------------------------ |
| any subset of a known list (0..n)     | `CheckboxGroup` (checkboxes)           | comma-separated `<Input>`      |
| exactly one of a known list           | `Select` (or radio group if ≤ 4)       | free-text `<Input>`            |
| a value with free text + suggestions  | `Input` with a `datalist`/combobox     | a bare `<Input>` with no hints |
| any of a known list of 50+ items      | searchable multi-select (ask first)   | a wall of 200 checkboxes       |

The checkbox component is `web/src/components/ui/checkbox-group.tsx`:

```tsx
<CheckboxGroup
  id="ap-deny"
  options={AGENT_TOOLS.map((tool) => ({
    value: tool.value,                                  // what is stored
    label: t(`agent.policy.tools.${tool.i18nKey}.label`),
    description: t(`agent.policy.tools.${tool.i18nKey}.description`),
    group: "optional heading",
  }))}
  value={denyTools}          // string[]
  onChange={setDenyTools}
  disabled={!canManage}
/>
```

It keeps the option order stable, offers "Select all / Clear all", groups under
headings, and **keeps values that are selected but missing from `options`**
(for example a value saved by an older release) so saving never drops data.

## Where the options come from

1. **The backend already exposes a catalogue** → fetch it with a TanStack Query
   hook and fetch lazily (`enabled` only when the dialog is open).
   Example: `usePermissionCatalog` (`GET /api/v1/permissions`) feeds the token
   scope picker. Prefer this when the list is large or changes often.
2. **The list is small and fixed in code** → put it in one typed constant next
   to the feature, with a comment pointing at the backend source of truth, and
   keep it in sync.
   Examples: `features/agent/tools.ts`, `features/billing/resourceTypes.ts`,
   `features/admin/roles.ts`.
3. **Neither exists and the backend should own it** → add a small read-only
   endpoint (OpenAPI first, then regenerate) rather than duplicating a long
   list in the frontend.

Never build the list by parsing user input.

## Rules

- **Labels go through `t()`.** The stored `value` is a machine slug and stays
  untranslated; the label and description are translated. i18next keys cannot
  contain dots, so map `compute.list_instances` → `compute_list_instances`.
- **Both locales** (`en` and `fa`) get every new key (`npm run i18n:check`).
- **Group long lists** with `group` (by module/area) and put the slug in a
  monospace label when the slug itself is what users recognise (scopes).
- **Scroll long lists** inside a bordered box (`max-h-64 overflow-y-auto`), not
  the whole dialog.
- **Say what "none selected" means** in the hint text (denylist: nothing hidden;
  token scopes: full account access). An empty selection must never be a silent
  surprise.
- **Disabled state** follows the permission check (`usePerm`).
- **Seeding async data**: set the selected array from the query in a
  `useEffect` (see the "Forms + async data" rules in `web/AGENTS.md`), and
  guard `null` arrays from the API (`data.denyTools ?? []`).
- **Dependent fields**: when a choice implies a sibling value (resource type →
  conventional unit), pre-fill or suggest it (placeholder), but let the user
  override and don't overwrite what they typed.
- **Accessibility**: each checkbox needs a `<Label htmlFor>`; the group needs a
  visible heading (`<Label>` / `<legend>`). `CheckboxGroup` does the per-item
  wiring; you provide the group label.
- **Server stays the authority.** Still validate on the backend; the list is a
  usability aid, not a security control.

## Anti-patterns (fix on sight)

```tsx
// ✘ typing a list the app already knows
<Input value={denyTools} placeholder="sample.destructive, compute.instance.delete" />
const list = input.split(",").map((s) => s.trim()).filter(Boolean);

// ✘ free-text for an enum
<Input placeholder="compute.cpu" {...register("resourceType")} />
```

## Checklist before you finish

- [ ] No `split(",")` / `join(", ")` for user-edited value lists.
- [ ] Options come from a backend catalogue or one typed constant.
- [ ] Labels/descriptions translated in `en` and `fa`.
- [ ] Unknown stored values are preserved.
- [ ] Empty selection explained in the hint.
- [ ] A test covers toggling, and that unknown values survive
      (`checkbox-group.test.tsx` is the model).

## Related

- `frontend-foundations` — stack, forms + async data, i18n rules.
- `ui-styling` — shadcn/ui patterns.
- `web/AGENTS.md` — hard rules.
