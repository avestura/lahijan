/**
 * PersonalAccessTokensCard tests.
 *
 * Mocks the api client + asserts:
 *   - empty state renders when there are no tokens.
 *   - list renders table rows for each token.
 *   - "Create" opens the dialog, accepts name, POSTs, and reveals the
 *     raw token in the one-shot reveal dialog.
 */
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { PropsWithChildren } from "react";

const mockGet = vi.fn<(...args: unknown[]) => Promise<unknown>>();
const mockPost = vi.fn<(...args: unknown[]) => Promise<unknown>>();
const mockDelete = vi.fn<(...args: unknown[]) => Promise<unknown>>();

vi.mock("@/lib/api/client", () => ({
  apiClient: {
    GET: (...args: unknown[]) => mockGet(...args),
    POST: (...args: unknown[]) => mockPost(...args),
    DELETE: (...args: unknown[]) => mockDelete(...args),
  },
}));

vi.mock("@/hooks/useToast", () => ({
  useToast: () => ({ toast: () => undefined, dismiss: () => undefined, toasts: [] }),
}));

import { PersonalAccessTokensCard } from "./PersonalAccessTokensCard";

function Wrapper({ children }: PropsWithChildren) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe("<PersonalAccessTokensCard />", () => {
  beforeEach(() => {
    mockGet.mockReset();
    mockPost.mockReset();
    mockDelete.mockReset();
  });

  it("renders the empty state when the list is empty", async () => {
    mockGet.mockResolvedValueOnce({ data: [], error: null, response: { status: 200 } });
    render(<PersonalAccessTokensCard />, { wrapper: Wrapper });
    expect(await screen.findByText("No tokens yet")).toBeInTheDocument();
  });

  it("renders table rows for each token", async () => {
    mockGet.mockResolvedValueOnce({
      data: [
        {
          id: "t1",
          name: "ci-deploy",
          scopes: ["compute.instance.start"],
          createdAt: "2026-01-01T00:00:00Z",
        },
      ],
      error: null,
      response: { status: 200 },
    });
    render(<PersonalAccessTokensCard />, { wrapper: Wrapper });
    expect(await screen.findByText("ci-deploy")).toBeInTheDocument();
    expect(await screen.findByText("compute.instance.start")).toBeInTheDocument();
  });

  it("creates a token and reveals the raw value once", async () => {
    mockGet.mockResolvedValueOnce({ data: [], error: null, response: { status: 200 } });
    mockPost.mockResolvedValueOnce({
      data: { id: "t2", name: "ci", token: "lahijan_pat_raw_value" },
      error: null,
      response: { status: 201 },
    });
    render(<PersonalAccessTokensCard />, { wrapper: Wrapper });
    await screen.findByText("No tokens yet");

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "New token" }));
    await user.type(screen.getByLabelText("Name"), "ci");
    await user.click(screen.getByRole("button", { name: "Create" }));

    expect(await screen.findByText("lahijan_pat_raw_value")).toBeInTheDocument();
    expect(mockPost).toHaveBeenCalledWith("/api/v1/auth/personal-access-tokens", {
      body: expect.objectContaining({ name: "ci", scopes: [] }),
    });
  });
});
