/**
 * Platform-admin query + mutation hooks: user management and external
 * directory connections (LDAP / SAML).
 *
 * Every mutation invalidates the affected list so the tables stay current;
 * the server emits an audit event for each one.
 */
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { components } from "@api-schema";

import { apiClient } from "@/lib/api/client";
import { queryKeys } from "@/lib/api/keys";

export type AdminUser = components["schemas"]["AdminUser"];
export type AdminUserPage = components["schemas"]["AdminUserPage"];
export type AdminUserCreateRequest = components["schemas"]["AdminUserCreateRequest"];
export type AdminUserUpdateRequest = components["schemas"]["AdminUserUpdateRequest"];
export type DirectoryConnection = components["schemas"]["DirectoryConnection"];
export type DirectoryConnectionRequest = components["schemas"]["DirectoryConnectionRequest"];
export type DirectoryTestResult = components["schemas"]["DirectoryTestResult"];
export type DirectorySyncResult = components["schemas"]["DirectorySyncResult"];
export type DirectoryGroupPage = components["schemas"]["DirectoryGroupPage"];

function fail(op: string, response: Response | undefined): Error {
  return new Error(`${op}: ${response?.status ?? "network"}`);
}

// ---------------------------------------------------------------------------
// Users
// ---------------------------------------------------------------------------

export interface AdminUsersFilters {
  q?: string;
  offset: number;
  limit: number;
}

/** useAdminUsers — GET /admin/users (paginated, optional search). */
export function useAdminUsers(filters: AdminUsersFilters) {
  return useQuery({
    queryKey: queryKeys.admin.users({ ...filters }),
    placeholderData: keepPreviousData,
    staleTime: 15_000,
    queryFn: async (): Promise<AdminUserPage> => {
      const { data, error, response } = await apiClient.GET("/api/v1/admin/users", {
        params: {
          query: {
            q: filters.q === "" ? undefined : filters.q,
            limit: filters.limit,
            offset: filters.offset,
          },
        },
      });
      if (error || !data) throw fail("admin.users.list", response);
      return data;
    },
  });
}

/** useCreateAdminUser — POST /admin/users. */
export function useCreateAdminUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: AdminUserCreateRequest): Promise<AdminUser> => {
      const { data, error, response } = await apiClient.POST("/api/v1/admin/users", { body });
      if (error || !data) throw fail("admin.users.create", response);
      return data;
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.admin.usersAll() }),
  });
}

/** useUpdateAdminUser — PATCH /admin/users/{userId}. */
export function useUpdateAdminUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: {
      userId: string;
      body: AdminUserUpdateRequest;
    }): Promise<AdminUser> => {
      const { data, error, response } = await apiClient.PATCH("/api/v1/admin/users/{userId}", {
        params: { path: { userId: input.userId } },
        body: input.body,
      });
      if (error || !data) throw fail("admin.users.update", response);
      return data;
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.admin.usersAll() }),
  });
}

/** useDeleteAdminUser — DELETE /admin/users/{userId}. */
export function useDeleteAdminUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (userId: string): Promise<void> => {
      const { error, response } = await apiClient.DELETE("/api/v1/admin/users/{userId}", {
        params: { path: { userId } },
      });
      if (error) throw fail("admin.users.delete", response);
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.admin.usersAll() }),
  });
}

/** useSetAdminUserRole — PUT /admin/users/{userId}/memberships/{tenantId}. */
export function useSetAdminUserRole() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: {
      userId: string;
      tenantId: string;
      role: string;
    }): Promise<AdminUser> => {
      const { data, error, response } = await apiClient.PUT(
        "/api/v1/admin/users/{userId}/memberships/{tenantId}",
        {
          params: { path: { userId: input.userId, tenantId: input.tenantId } },
          body: { role: input.role },
        },
      );
      if (error || !data) throw fail("admin.users.role", response);
      return data;
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.admin.usersAll() }),
  });
}

// ---------------------------------------------------------------------------
// Directory connections
// ---------------------------------------------------------------------------

/** useDirectoryConnections — GET /admin/directory/connections. */
export function useDirectoryConnections() {
  return useQuery({
    queryKey: queryKeys.admin.connections(),
    staleTime: 15_000,
    queryFn: async (): Promise<DirectoryConnection[]> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/directory/connections",
        {},
      );
      if (error || !data) throw fail("admin.directory.list", response);
      return data.items;
    },
  });
}

/** useCreateDirectoryConnection — POST /admin/directory/connections. */
export function useCreateDirectoryConnection() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (body: DirectoryConnectionRequest): Promise<DirectoryConnection> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/directory/connections",
        { body },
      );
      if (error || !data) throw fail("admin.directory.create", response);
      return data;
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.admin.directory() }),
  });
}

/** useUpdateDirectoryConnection — PATCH /admin/directory/connections/{id}. */
export function useUpdateDirectoryConnection() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (input: {
      id: string;
      body: DirectoryConnectionRequest;
    }): Promise<DirectoryConnection> => {
      const { data, error, response } = await apiClient.PATCH(
        "/api/v1/admin/directory/connections/{connectionId}",
        { params: { path: { connectionId: input.id } }, body: input.body },
      );
      if (error || !data) throw fail("admin.directory.update", response);
      return data;
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: queryKeys.admin.directory() }),
  });
}

/** useDeleteDirectoryConnection — DELETE /admin/directory/connections/{id}. */
export function useDeleteDirectoryConnection() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string): Promise<void> => {
      const { error, response } = await apiClient.DELETE(
        "/api/v1/admin/directory/connections/{connectionId}",
        { params: { path: { connectionId: id } } },
      );
      if (error) throw fail("admin.directory.delete", response);
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.admin.directory() });
      void qc.invalidateQueries({ queryKey: queryKeys.admin.usersAll() });
    },
  });
}

/**
 * useTestDirectoryConnection — POST /admin/directory/test. Pass a saved
 * connection id, a draft, or both (a draft with an id reuses the stored bind
 * password). A failed check resolves with ok=false.
 */
export function useTestDirectoryConnection() {
  return useMutation({
    mutationFn: async (input: {
      connectionId?: string;
      connection?: DirectoryConnectionRequest;
    }): Promise<DirectoryTestResult> => {
      const { data, error, response } = await apiClient.POST("/api/v1/admin/directory/test", {
        body: input,
      });
      if (error || !data) throw fail("admin.directory.test", response);
      return data;
    },
  });
}

/** useSyncDirectoryConnection — POST /admin/directory/connections/{id}/sync. */
export function useSyncDirectoryConnection() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: async (id: string): Promise<DirectorySyncResult> => {
      const { data, error, response } = await apiClient.POST(
        "/api/v1/admin/directory/connections/{connectionId}/sync",
        { params: { path: { connectionId: id } } },
      );
      if (error || !data) throw fail("admin.directory.sync", response);
      return data;
    },
    // Success or failure, the connection's last-sync status changed.
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: queryKeys.admin.directory() });
      void qc.invalidateQueries({ queryKey: queryKeys.admin.usersAll() });
    },
  });
}

/** useDirectoryGroups — GET /admin/directory/connections/{id}/groups. */
export function useDirectoryGroups(id: string | undefined, offset: number, limit: number) {
  return useQuery({
    queryKey: id ? queryKeys.admin.groups(id, { offset, limit }) : ["admin", "directory", "off"],
    enabled: !!id,
    placeholderData: keepPreviousData,
    queryFn: async (): Promise<DirectoryGroupPage> => {
      const { data, error, response } = await apiClient.GET(
        "/api/v1/admin/directory/connections/{connectionId}/groups",
        {
          params: {
            path: { connectionId: id! },
            query: { offset, limit },
          },
        },
      );
      if (error || !data) throw fail("admin.directory.groups", response);
      return data;
    },
  });
}
