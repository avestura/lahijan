/**
 * ROLE_OPTIONS — the built-in role slugs a membership can hold
 * (internal/app/lahijan/auth/rbac/roles.go). The label is the i18n key
 * `admin.roles.<i18nKey>`.
 */
export interface RoleOption {
  value: string;
  i18nKey: string;
}

export const ROLE_OPTIONS: readonly RoleOption[] = [
  { value: "platform.admin", i18nKey: "platform_admin" },
  { value: "tenant.owner", i18nKey: "tenant_owner" },
  { value: "tenant.admin", i18nKey: "tenant_admin" },
  { value: "tenant.member", i18nKey: "tenant_member" },
  { value: "tenant.viewer", i18nKey: "tenant_viewer" },
];

/** roleKey maps a role slug to its i18n key suffix (dots are not allowed). */
export function roleKey(slug: string): string {
  return slug.replace(/\./g, "_");
}
