/**
 * LoginForm component test.
 *
 * Verifies the form:
 *   - Renders the localized labels + submit button.
 *   - Validates required fields (shows localized "required" messages).
 *   - Submits valid credentials through the mocked apiClient.
 *   - Renders the localized "invalid email or password" on 401.
 */
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

import { LoginForm } from "@/features/auth/LoginForm";

const mockPost = vi.fn<(...args: unknown[]) => Promise<unknown>>();

vi.mock("@/lib/api/client", () => ({
  apiClient: {
    POST: (...args: unknown[]) => mockPost(...args),
  },
}));

import { useSessionStore } from "@/lib/stores/session-store";

function withQueryClient(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

describe("<LoginForm />", () => {
  beforeEach(() => {
    mockPost.mockReset();
    useSessionStore.getState().reset();
  });

  it("renders the localized title, labels, and submit", () => {
    render(withQueryClient(<LoginForm />));
    expect(screen.getByText("Sign in")).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toBeInTheDocument();
    expect(screen.getByLabelText("Password")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sign in" })).toBeEnabled();
  });

  it("shows required-field errors when submitted empty", async () => {
    const user = userEvent.setup();
    render(withQueryClient(<LoginForm />));
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    // zod's `z.string().email()` fails empty input with invalid_string,
    // which the LoginForm surfaces as the "invalid email" localized copy.
    expect(await screen.findByText("Enter a valid email address")).toBeInTheDocument();
    expect(await screen.findByText("Password must be at least 8 characters")).toBeInTheDocument();
    expect(mockPost).not.toHaveBeenCalled();
  });

  it("submits valid credentials and populates the session store", async () => {
    const user = userEvent.setup();
    mockPost.mockResolvedValueOnce({
      data: { user: { id: "u1", email: "user@example.com" } },
      error: null,
      response: { status: 200 },
    });
    render(withQueryClient(<LoginForm />));

    await user.type(screen.getByLabelText("Email"), "user@example.com");
    await user.type(screen.getByLabelText("Password"), "password1");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(mockPost).toHaveBeenCalledWith("/api/v1/auth/login", {
      body: { email: "user@example.com", password: "password1" },
    });
    expect(useSessionStore.getState().user?.email).toBe("user@example.com");
  });

  it("renders the localized invalid-credentials message on 401", async () => {
    const user = userEvent.setup();
    mockPost.mockResolvedValueOnce({
      data: null,
      error: { code: "unauthorized" },
      response: { status: 401 },
    });
    render(withQueryClient(<LoginForm />));

    await user.type(screen.getByLabelText("Email"), "user@example.com");
    await user.type(screen.getByLabelText("Password"), "password1");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByText("Invalid email or password")).toBeInTheDocument();
    expect(useSessionStore.getState().user).toBeNull();
  });
});
