/**
 * Compute api hook tests.
 *
 * Mocks @/lib/api/client and verifies the query/mutation hooks hit the
 * expected paths with the right bodies. We don't exercise the network
 * layer here — the integration story is covered by the backend's own
 * e2e tests in WS-14.
 */
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
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

// Mock useToast so mutations don't blow up on the toast queue.
vi.mock("@/hooks/useToast", () => ({
  useToast: () => ({ toast: () => undefined, dismiss: () => undefined, toasts: [] }),
}));

import {
  useComputeInstances,
  useCreateInstance,
  useDeleteInstance,
  useLifecycle,
  classifyStatus,
} from "./api";
import { useSessionStore } from "@/lib/stores/session-store";

const TENANT = "00000000-0000-0000-0000-000000000001";

function Wrapper({ children }: PropsWithChildren): ReactNode {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe("classifyStatus", () => {
  it.each([
    ["Running", "running"],
    ["running", "running"],
    ["Stopped", "stopped"],
    ["Frozen", "frozen"],
    ["Starting", "other"],
    [undefined, "other"],
  ] as const)("classifies %s as %s", (input, expected) => {
    expect(classifyStatus(input)).toBe(expected);
  });
});

describe("useComputeInstances", () => {
  beforeEach(() => {
    mockGet.mockReset();
    mockPost.mockReset();
    mockDelete.mockReset();
    useSessionStore.getState().reset();
    useSessionStore.setState({ currentTenantId: TENANT });
  });
  afterEach(() => {
    useSessionStore.getState().reset();
  });

  it("calls GET /api/v1/compute/instances and unwraps items", async () => {
    mockGet.mockResolvedValueOnce({
      data: { items: [{ id: "i1", name: "alpha" }], total: 1, limit: 20, offset: 0 },
      error: null,
      response: { status: 200 },
    });
    const { result } = renderHook(() => useComputeInstances(TENANT), {
      wrapper: Wrapper,
    });
    await waitFor(() => expect(result.current.data).toBeDefined());
    expect(mockGet).toHaveBeenCalledWith("/api/v1/compute/instances", {});
    expect(result.current.data?.[0]?.name).toBe("alpha");
  });

  it("throws when the API errors", async () => {
    mockGet.mockResolvedValueOnce({
      data: null,
      error: { code: "internal" },
      response: { status: 500 },
    });
    const { result } = renderHook(() => useComputeInstances(TENANT), {
      wrapper: Wrapper,
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toBeInstanceOf(Error);
  });
});

describe("useCreateInstance", () => {
  beforeEach(() => {
    mockGet.mockReset();
    mockPost.mockReset();
    mockDelete.mockReset();
    useSessionStore.getState().reset();
    useSessionStore.setState({ currentTenantId: TENANT });
  });
  afterEach(() => {
    useSessionStore.getState().reset();
  });

  it("POSTs the body with limits.cpu / limits.memory / root disk size", async () => {
    mockPost.mockResolvedValueOnce({
      data: { id: "i1", name: "alpha" },
      error: null,
      response: { status: 201 },
    });
    const { result } = renderHook(() => useCreateInstance(TENANT), {
      wrapper: Wrapper,
    });

    await act(async () => {
      await result.current.mutateAsync({
        name: "alpha",
        type: "container",
        imageAlias: "ubuntu/24.04",
        profile: "default",
        cpu: 2,
        memoryMiB: 1024,
        diskGiB: 20,
      });
    });

    expect(mockPost).toHaveBeenCalledWith("/api/v1/compute/instances", {
      body: expect.objectContaining({
        name: "alpha",
        imageAlias: "ubuntu/24.04",
        profiles: ["default"],
        config: { "limits.cpu": "2", "limits.memory": "1024MiB" },
        devices: { root: { type: "disk", path: "/", size: "20GiB" } },
      }),
    });
  });
});

describe("useLifecycle", () => {
  beforeEach(() => {
    mockGet.mockReset();
    mockPost.mockReset();
    mockDelete.mockReset();
    useSessionStore.getState().reset();
    useSessionStore.setState({ currentTenantId: TENANT });
  });
  afterEach(() => {
    useSessionStore.getState().reset();
  });

  it("POSTs the action with the right path params", async () => {
    mockPost.mockResolvedValueOnce({
      data: { id: "i1", status: "Running" },
      error: null,
      response: { status: 200 },
    });
    const { result } = renderHook(() => useLifecycle(TENANT), {
      wrapper: Wrapper,
    });

    await act(async () => {
      await result.current.mutateAsync({ instanceId: "i1", action: "start" });
    });

    expect(mockPost).toHaveBeenCalledWith("/api/v1/compute/instances/{instanceId}/{action}", {
      params: expect.objectContaining({
        path: { instanceId: "i1", action: "start" },
        query: { force: false },
      }),
    });
  });
});

describe("useDeleteInstance", () => {
  beforeEach(() => {
    mockGet.mockReset();
    mockPost.mockReset();
    mockDelete.mockReset();
    useSessionStore.getState().reset();
    useSessionStore.setState({ currentTenantId: TENANT });
  });
  afterEach(() => {
    useSessionStore.getState().reset();
  });

  it("DELETEs with the force flag in the query string", async () => {
    mockDelete.mockResolvedValueOnce({
      error: null,
      response: { status: 204 },
    });
    const { result } = renderHook(() => useDeleteInstance(TENANT), {
      wrapper: Wrapper,
    });

    await act(async () => {
      await result.current.mutateAsync({ instanceId: "i1", force: true });
    });

    expect(mockDelete).toHaveBeenCalledWith("/api/v1/compute/instances/{instanceId}", {
      params: expect.objectContaining({
        path: { instanceId: "i1" },
        query: { force: true },
      }),
    });
  });
});
