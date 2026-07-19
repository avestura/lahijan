/**
 * no-jsx-literal-strings
 *
 * Flags bare string literals that appear as JSX children or in user-facing
 * props (title, aria-label, placeholder, alt). Allows:
 *   - whitespace-only strings
 *   - strings that look like i18n keys (dotted identifiers: "nav.dashboard")
 *   - strings used as nothing but separators (":", "|", "·", "/", "—")
 *   - strings inside <Trans>...</Trans> (handled separately by react-i18next)
 *
 * The intent is to catch English copy that should go through `t("key")`.
 */

const USER_FACING_PROPS = new Set([
  "title",
  "aria-label",
  "aria-description",
  "placeholder",
  "alt",
  "label",
  "value",
  "subtitle",
  "description",
  "hint",
  "helperText",
  "tooltip",
]);

// Looks like "area.scope.label" or "nav.dashboard.title" — at least one dot,
// and every segment is a valid JS identifier.
const I18N_KEY = /^[a-zA-Z_$][a-zA-Z0-9_$]*(\.[a-zA-Z_$][a-zA-Z0-9_$]*)+$/;

const SEPARATOR_OR_SYMBOL = /^[\s:|·\-_/—–.()*&%]+$/;

function isAllowed(text) {
  if (text.trim() === "") return true;
  if (SEPARATOR_OR_SYMBOL.test(text)) return true;
  if (I18N_KEY.test(text.trim())) return true;
  // Pure number or single punctuation -> allowed.
  if (/^[\d.,\s]+$/.test(text)) return true;
  return false;
}

export default {
  meta: {
    type: "problem",
    docs: {
      description:
        "Disallow hardcoded user-facing string literals in JSX (use react-i18next t() instead)",
    },
    schema: [
      {
        type: "object",
        properties: {
          allowProps: {
            type: "boolean",
            default: true,
          },
        },
        additionalProperties: false,
      },
    ],
    messages: {
      literalString:
        "Hardcoded string '{{text}}' is not allowed in JSX. Use t('{{text}}') from react-i18next instead.",
      literalProp:
        "Hardcoded string '{{text}}' is not allowed in prop '{{prop}}'. Use t('{{text}}') instead.",
    },
  },

  create(context) {
    const options = context.options[0] ?? {};
    const allowProps = options.allowProps !== false;

    function checkLiteral(node, prop = null) {
      if (!node) return;
      if (node.type !== "Literal") return;
      if (typeof node.value !== "string") return;
      const text = String(node.value);
      if (isAllowed(text)) return;
      if (prop) {
        if (!allowProps) return;
        if (!USER_FACING_PROPS.has(prop.name)) return;
        context.report({
          node,
          messageId: "literalProp",
          data: { text, prop: prop.name },
        });
        return;
      }
      context.report({
        node,
        messageId: "literalString",
        data: { text },
      });
    }

    function walkChildren(children) {
      for (const child of children) {
        // Bare string literal as a JSX child.
        if (child.type === "Literal") {
          checkLiteral(child);
        }
        // {"raw string"} expression containers also need to be checked.
        if (child.type === "JSXExpressionContainer") {
          checkLiteral(child.expression);
        }
        // JSXText with non-whitespace content (e.g. <p>Hello</p>) — the parser
        // typically hands these as JSXText, not Literal.
        if (child.type === "JSXText") {
          const text = String(child.value ?? "");
          if (isAllowed(text)) continue;
          context.report({
            node: child,
            messageId: "literalString",
            data: { text: text.trim() },
          });
        }
      }
    }

    return {
      JSXElement(node) {
        walkChildren(node.children);
      },
      JSXFragment(node) {
        walkChildren(node.children);
      },
      JSXAttribute(node) {
        if (!allowProps) return;
        const name = node.name;
        if (!name || name.type !== "JSXIdentifier") return;
        // Only check user-facing props.
        if (!USER_FACING_PROPS.has(name.name)) return;
        const value = node.value;
        if (!value) return;
        if (value.type === "Literal") {
          checkLiteral(value, name);
        }
        if (value.type === "JSXExpressionContainer" && value.expression.type === "Literal") {
          checkLiteral(value.expression, name);
        }
      },
    };
  },
};
