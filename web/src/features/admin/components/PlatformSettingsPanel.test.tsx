/**
 * PlatformSettingsPanel tests: shows the effective registration setting and its
 * source, saves the toggle, and resets to the configured default.
 */
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { PropsWithChildren } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mockGet = vi.fn<(...args: unknown[]) => Promise<unknown>>();
const mockPut = vi.fn<(...args: unknown[]) => Promise<unknown>>();

vi.mock("@/lib/api/client", () => ({
  apiClient: {
    GET: (...args: unknown[]) => mockGet(...args),
    PUT: (...args: unknown[]) => mockPut(...args),
  },
}));
vi.mock("@/hooks/useToast", () => ({
  useToast: () => ({ toast: () => undefined, dismiss: () => undefined, toasts: [] }),
}));
vi.mock("@/lib/perm", () => ({ usePerm: () => ({ hasPerm: true, isLoading: false }) }));

import { PlatformSettingsPanel } from "./PlatformSettingsPanel";

function Wrapper({ children }: PropsWithChildren) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function settings(over: Record<string, unknown> = {}) {
  return {
    data: {
      registrationEnabled: true,
      registrationDefault: true,
      registrationOverridden: false,
      ...over,
    },
    error: undefined,
    response: { status: 200 },
  };
}

describe("<PlatformSettingsPanel />", () => {
  beforeEach(() => {
    mockGet.mockReset();
    mockPut.mockReset();
  });

  it("shows registration open and that the configured default applies", async () => {
    mockGet.mockResolvedValue(settings());
    render(<PlatformSettingsPanel />, { wrapper: Wrapper });

    expect(await screen.findByLabelText("Allow people to register")).toBeChecked();
    expect(screen.getByText("Open")).toBeInTheDocument();
    expect(screen.getByText(/configured default applies/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Use the configured default" })).toBeNull();
  });

  it("turns registration off", async () => {
    mockGet.mockResolvedValue(settings());
    mockPut.mockResolvedValue(
      settings({ registrationEnabled: false, registrationOverridden: true }),
    );
    const user = userEvent.setup();
    render(<PlatformSettingsPanel />, { wrapper: Wrapper });

    await user.click(await screen.findByLabelText("Allow people to register"));

    await waitFor(() =>
      expect(mockPut).toHaveBeenCalledWith("/api/v1/admin/settings", {
        body: { registrationEnabled: false },
      }),
    );
    expect(await screen.findByLabelText("Allow people to register")).not.toBeChecked();
    expect(screen.getByText("Closed")).toBeInTheDocument();
  });

  it("offers to reset a saved choice to the configured default", async () => {
    mockGet.mockResolvedValue(
      settings({ registrationEnabled: false, registrationOverridden: true }),
    );
    mockPut.mockResolvedValue(settings());
    const user = userEvent.setup();
    render(<PlatformSettingsPanel />, { wrapper: Wrapper });

    await user.click(await screen.findByRole("button", { name: "Use the configured default" }));

    await waitFor(() =>
      expect(mockPut).toHaveBeenCalledWith("/api/v1/admin/settings", {
        body: { resetRegistration: true },
      }),
    );
  });
});
