/**
 * no-physical-directional-utilities
 *
 * Flags Tailwind physical-direction utility classes in JSX `className`
 * strings. RTL languages (Persian, Arabic, Hebrew) flip the layout; the
 * physical utilities do NOT flip, which produces broken RTL UI. Logical
 * equivalents do:
 *
 *   ml-/mr-/pl-/pr-/left-/right-  -> ms-/me-/ps-/pe-/start-/end-
 *
 * Allowed exceptions:
 *   - `left-0`/`right-0` used in absolute-positioning context where the
 *     position is geometric, not directional (e.g. a tooltip arrow). We
 *     still flag these — the engineer should justify via eslint-disable
 *     with a comment (per the conventions doc).
 */

const PHYSICAL = /\b(ml|mr|pl|pr|left|right)-(-?\d+(?:\.\d+)?|\[.+?\])\b/;
// Don't flag negative-margin logical vs physical conflicts — only flag clear
// physical layout utilities.

export default {
  meta: {
    type: "problem",
    docs: {
      description:
        "Disallow Tailwind physical layout utilities (ml-, mr-, pl-, pr-, left-, right-). Use logical ms-/me-/ps-/pe-/start-/end- for RTL safety.",
    },
    schema: [],
    messages: {
      physicalUtility:
        "Tailwind utility '{{token}}' is physical and breaks RTL. Use the logical equivalent (ms-/me-/ps-/pe-/start-/end-).",
    },
  },

  create(context) {
    function checkString(node, raw) {
      if (typeof raw !== "string") return;
      const match = PHYSICAL.exec(raw);
      if (!match) return;
      context.report({
        node,
        messageId: "physicalUtility",
        data: { token: match[1] + "-" + match[2] },
      });
    }

    return {
      JSXAttribute(node) {
        if (node.name?.type !== "JSXIdentifier") return;
        if (node.name.name !== "className") return;
        const value = node.value;
        if (!value) return;
        if (value.type === "Literal") {
          checkString(value, String(value.value ?? ""));
        }
        if (value.type === "JSXExpressionContainer" && value.expression.type === "Literal") {
          checkString(value.expression, String(value.expression.value ?? ""));
        }
        // Template-literal className (e.g. className={`flex ${x}`}).
        if (
          value.type === "JSXExpressionContainer" &&
          value.expression.type === "TemplateLiteral"
        ) {
          for (const quasi of value.expression.quasis ?? []) {
            checkString(quasi, String(quasi?.value?.cooked ?? ""));
          }
        }
      },
    };
  },
};
