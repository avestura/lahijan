/**
 * UsersTable — searchable, paginated list of every user on the platform.
 *
 * Shared by /admin/users (with edit / delete actions) and the billing admin
 * "Users" tab (with a "manage billing" action), so each page only supplies
 * its own row actions.
 */
import { useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { SearchIcon, UsersIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Pagination } from "@/components/ui/pagination";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { EmptyState } from "@/components/layout/EmptyState";
import { ErrorState } from "@/components/layout/ErrorState";
import { LoadingState } from "@/components/layout/LoadingState";
import { useAdminUsers, type AdminUser } from "../api";
import { roleKey } from "../roles";

const PAGE_SIZE = 15;
const SEARCH_DEBOUNCE_MS = 300;

interface Props {
  /** Extra cell rendered at the end of each row. */
  renderActions?: (user: AdminUser) => ReactNode;
  /** Highlights the row of this user id. */
  selectedId?: string | null;
  /** Header for the actions column; omit to hide the column. */
  actionsLabel?: string;
}

export function UsersTable({ renderActions, selectedId, actionsLabel }: Props) {
  const { t } = useTranslation();
  const [search, setSearch] = useState("");
  const [q, setQ] = useState("");
  const [offset, setOffset] = useState(0);

  // Debounce the search box and jump back to the first page on a new query.
  useEffect(() => {
    const id = setTimeout(() => {
      setQ(search.trim());
      setOffset(0);
    }, SEARCH_DEBOUNCE_MS);
    return () => clearTimeout(id);
  }, [search]);

  const query = useAdminUsers({ q, offset, limit: PAGE_SIZE });
  const items = query.data?.items ?? [];

  return (
    <div className="space-y-3">
      <div className="relative max-w-sm">
        <SearchIcon className="pointer-events-none absolute start-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t("admin.users.search")}
          aria-label={t("admin.users.search")}
          className="ps-8"
        />
      </div>

      {query.isLoading ? (
        <LoadingState rows={5} />
      ) : query.error ? (
        <ErrorState
          message={t("common.error")}
          retryLabel={t("common.retry")}
          onRetry={() => void query.refetch()}
        />
      ) : items.length === 0 ? (
        <EmptyState
          icon={UsersIcon}
          title={t("admin.users.empty.title")}
          description={q ? t("admin.users.empty.search") : t("admin.users.empty.body")}
        />
      ) : (
        <>
          <div className="border border-border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("admin.users.columns.user")}</TableHead>
                  <TableHead>{t("admin.users.columns.status")}</TableHead>
                  <TableHead>{t("admin.users.columns.roles")}</TableHead>
                  <TableHead>{t("admin.users.columns.source")}</TableHead>
                  <TableHead>{t("admin.users.columns.created")}</TableHead>
                  {actionsLabel && <TableHead>{actionsLabel}</TableHead>}
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((u) => (
                  <TableRow key={u.id} data-state={selectedId === u.id ? "selected" : undefined}>
                    <TableCell>
                      <div className="text-sm font-medium">{u.email}</div>
                      {u.displayName && (
                        <div className="text-xs text-muted-foreground">{u.displayName}</div>
                      )}
                    </TableCell>
                    <TableCell>
                      <Badge variant={u.isActive ? "success" : "muted"}>
                        {u.isActive ? t("admin.users.active") : t("admin.users.disabled")}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {uniqueRoles(u).map((r) => (
                          <Badge key={r} variant="outline">
                            {t(`admin.roles.${roleKey(r)}`, { defaultValue: r })}
                          </Badge>
                        ))}
                        {u.memberships.length === 0 && (
                          <span className="text-xs text-muted-foreground">{t("common.none")}</span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-xs">
                      {u.directorySource
                        ? u.directorySource.name
                        : u.hasPassword
                          ? t("admin.users.sourceLocal")
                          : t("admin.users.sourceNoPassword")}
                    </TableCell>
                    <TableCell className="text-xs text-muted-foreground">
                      {new Date(u.createdAt).toLocaleDateString()}
                    </TableCell>
                    {actionsLabel && (
                      <TableCell>
                        <div className="flex flex-wrap gap-2">{renderActions?.(u)}</div>
                      </TableCell>
                    )}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <Pagination
            offset={offset}
            limit={PAGE_SIZE}
            total={query.data?.total ?? 0}
            onChange={setOffset}
            disabled={query.isFetching}
          />
        </>
      )}
    </div>
  );
}

function uniqueRoles(u: AdminUser): string[] {
  return [...new Set(u.memberships.map((m) => m.role))];
}
