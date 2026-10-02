/**
 * UsersTable tests: renders a page of users, searches (debounced) and pages.
 */
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { PropsWithChildren } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mockGet = vi.fn<(...args: unknown[]) => Promise<unknown>>();

vi.mock("@/lib/api/client", () => ({
  apiClient: { GET: (...args: unknown[]) => mockGet(...args) },
}));

import { UsersTable } from "./UsersTable";

function Wrapper({ children }: PropsWithChildren) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function user(id: string, email: string, extra: Record<string, unknown> = {}) {
  return {
    id,
    email,
    locale: "en",
    isActive: true,
    emailVerified: true,
    hasPassword: true,
    createdAt: "2026-01-01T00:00:00Z",
    memberships: [{ tenantId: "t1", tenantSlug: "acme", tenantName: "Acme", role: "tenant.admin" }],
    ...extra,
  };
}

function page(items: unknown[], total: number) {
  return {
    data: { items, total, limit: 15, offset: 0 },
    error: undefined,
    response: { status: 200 },
  };
}

describe("<UsersTable />", () => {
  beforeEach(() => mockGet.mockReset());

  it("renders users with status, role and source", async () => {
    mockGet.mockResolvedValue(
      page(
        [
          user("1", "ann@example.com"),
          user("2", "bob@example.com", {
            isActive: false,
            hasPassword: false,
            directorySource: { connectionId: "c1", name: "corp-ldap", kind: "ldap" },
          }),
        ],
        2,
      ),
    );
    render(<UsersTable />, { wrapper: Wrapper });

    expect(await screen.findByText("ann@example.com")).toBeInTheDocument();
    expect(screen.getByText("bob@example.com")).toBeInTheDocument();
    expect(screen.getByText("Disabled")).toBeInTheDocument();
    expect(screen.getByText("corp-ldap")).toBeInTheDocument();
    expect(screen.getAllByText("Admin").length).toBeGreaterThan(0);
  });

  it("shows the empty state when nothing matches", async () => {
    mockGet.mockResolvedValue(page([], 0));
    render(<UsersTable />, { wrapper: Wrapper });
    expect(await screen.findByText("No users found")).toBeInTheDocument();
  });

  it("requests the next page with the right offset", async () => {
    mockGet.mockResolvedValue(page([user("1", "ann@example.com")], 40));
    const u = userEvent.setup();
    render(<UsersTable />, { wrapper: Wrapper });
    await screen.findByText("ann@example.com");

    await u.click(screen.getByRole("button", { name: /next/i }));
    await waitFor(() =>
      expect(mockGet).toHaveBeenLastCalledWith(
        "/api/v1/admin/users",
        expect.objectContaining({
          params: { query: expect.objectContaining({ offset: 15, limit: 15 }) },
        }),
      ),
    );
  });

  it("searches after the debounce and resets to the first page", async () => {
    mockGet.mockResolvedValue(page([user("1", "ann@example.com")], 1));
    const u = userEvent.setup();
    render(<UsersTable />, { wrapper: Wrapper });
    await screen.findByText("ann@example.com");

    await u.type(screen.getByRole("textbox", { name: "Search by email or name" }), "ann");
    await waitFor(() =>
      expect(mockGet).toHaveBeenLastCalledWith(
        "/api/v1/admin/users",
        expect.objectContaining({
          params: { query: expect.objectContaining({ q: "ann", offset: 0 }) },
        }),
      ),
    );
  });
});
