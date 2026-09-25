/**
 * TenantSwitcher tests.
 *
 * Verifies the switcher:
 *   - renders nothing for a user with a single membership.
 *   - renders the dropdown + tenant rows for a user with multiple
 *     memberships.
 *   - clicking a row updates the session store's currentTenantId,
 *     which is what the API middleware + tenant-scoped query keys
 *     consume.
 */
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/api/client", () => ({
  apiClient: { GET: vi.fn(), POST: vi.fn(), DELETE: vi.fn() },
}));

import { TenantSwitcher } from "./TenantSwitcher";
import { useSessionStore } from "@/lib/stores/session-store";

const TENANT_A = "00000000-0000-0000-0000-000000000001";
const TENANT_B = "00000000-0000-0000-0000-000000000002";

describe("<TenantSwitcher />", () => {
  beforeEach(() => {
    useSessionStore.getState().reset();
  });

  it("renders nothing when the user has only one membership", () => {
    useSessionStore.setState({
      user: {
        id: "u1",
        email: "user@example.com",
        memberships: [{ tenantId: TENANT_A, role: "member" }],
      },
      currentTenantId: TENANT_A,
      status: "authenticated",
    });
    const { container } = render(<TenantSwitcher />);
    expect(container.firstChild).toBeNull();
  });

  it("renders the dropdown with both tenants", async () => {
    useSessionStore.setState({
      user: {
        id: "u1",
        email: "user@example.com",
        memberships: [
          { tenantId: TENANT_A, role: "member" },
          { tenantId: TENANT_B, role: "admin" },
        ],
      },
      currentTenantId: TENANT_A,
      status: "authenticated",
    });

    const user = userEvent.setup();
    render(<TenantSwitcher />);
    await user.click(screen.getByRole("button", { name: "Switch tenant" }));

    // Both tenant ids render.
    expect(screen.getAllByText(TENANT_A).length).toBeGreaterThan(0);
    expect(screen.getAllByText(TENANT_B).length).toBeGreaterThan(0);
  });

  it("clicking a tenant updates the session store", async () => {
    useSessionStore.setState({
      user: {
        id: "u1",
        email: "user@example.com",
        memberships: [
          { tenantId: TENANT_A, role: "member" },
          { tenantId: TENANT_B, role: "admin" },
        ],
      },
      currentTenantId: TENANT_A,
      status: "authenticated",
      roles: ["member"],
    });

    const user = userEvent.setup();
    render(<TenantSwitcher />);
    await user.click(screen.getByRole("button", { name: "Switch tenant" }));

    // Find the dropdown item for tenant B (the row that contains its id).
    const tenantBRadio = screen.getAllByText(TENANT_B)[0]!.closest('[role="menuitemradio"]');
    expect(tenantBRadio).not.toBeNull();
    await user.click(tenantBRadio!);

    expect(useSessionStore.getState().currentTenantId).toBe(TENANT_B);
    // roles should be re-derived from the new tenant's membership.
    expect(useSessionStore.getState().roles).toContain("admin");
  });
});
