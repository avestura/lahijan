/**
 * DNSZoneList — table of zones in the active tenant.
 *
 * Renders the loading, empty, error, and populated states per the
 * design system. The DNSSEC toggle is gated by `dns.zone.update`;
 * the delete action is gated by `dns.zone.delete`. The list is
 * filtered by the parent's free-text filter.
 */
import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";
import { MoreHorizontalIcon, Trash2Icon, GlobeIcon } from "lucide-react";
import type { components } from "@api-schema";

import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/layout/EmptyState";
import { LoadingState } from "@/components/layout/LoadingState";
import { ErrorState } from "@/components/layout/ErrorState";
import { usePerm } from "@/lib/perm";
import { useDeleteDNSZone } from "../api";

type DNSZone = components["schemas"]["DNSZone"];

interface Props {
  zones: DNSZone[] | undefined;
  isLoading: boolean;
  error: Error | null;
  onRetry: () => void;
}

export function DNSZoneList({ zones, isLoading, error, onRetry }: Props) {
  const { t } = useTranslation();
  const destroy = useDeleteDNSZone(null);
  const { hasPerm: canDelete } = usePerm("dns.zone.delete");

  if (isLoading) {
    return <LoadingState rows={4} />;
  }
  if (error) {
    return (
      <ErrorState message={t("common.error")} retryLabel={t("common.retry")} onRetry={onRetry} />
    );
  }
  if (!zones || zones.length === 0) {
    return (
      <EmptyState
        icon={GlobeIcon}
        title={t("dns.list.empty.title")}
        description={t("dns.list.empty.body")}
      />
    );
  }

  return (
    <div className="rounded-md border border-border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("dns.list.columns.name")}</TableHead>
            <TableHead>{t("dns.list.columns.kind")}</TableHead>
            <TableHead>{t("dns.list.columns.dnssec")}</TableHead>
            <TableHead>{t("dns.list.columns.created")}</TableHead>
            <TableHead className="text-end">{t("dns.list.columns.actions")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {zones.map((zone) => (
            <TableRow key={zone.id}>
              <TableCell>
                <Link
                  to="/dns/$zoneId"
                  params={{ zoneId: zone.id }}
                  className="font-medium hover:underline"
                >
                  {zone.name}
                </Link>
                {zone.description ? (
                  <p className="line-clamp-1 text-xs text-muted-foreground">{zone.description}</p>
                ) : null}
              </TableCell>
              <TableCell>
                <Badge variant="outline">{t(`dns.kinds.${zone.kind}`)}</Badge>
              </TableCell>
              <TableCell>
                <Badge variant={zone.isDnssecEnabled ? "default" : "secondary"}>
                  {zone.isDnssecEnabled ? t("dns.dnssec.on") : t("dns.dnssec.off")}
                </Badge>
              </TableCell>
              <TableCell className="text-xs text-muted-foreground">
                {new Date(zone.createdAt).toLocaleDateString()}
              </TableCell>
              <TableCell className="text-end">
                <div className="flex items-center justify-end gap-1">
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button
                        size="icon"
                        variant="ghost"
                        aria-label={t("dns.list.columns.actions")}
                      >
                        <MoreHorizontalIcon className="h-4 w-4" />
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end">
                      <DropdownMenuLabel>{t("dns.title")}</DropdownMenuLabel>
                      <DropdownMenuSeparator />
                      <DropdownMenuItem asChild>
                        <Link to="/dns/$zoneId" params={{ zoneId: zone.id }}>
                          {t("dns.detail.tabs.records")}
                        </Link>
                      </DropdownMenuItem>
                      {canDelete && (
                        <DropdownMenuItem
                          className="text-destructive focus:text-destructive"
                          onClick={() => destroy.mutate({ zoneId: zone.id })}
                        >
                          <Trash2Icon className="me-2 h-4 w-4" />
                          {t("common.delete")}
                        </DropdownMenuItem>
                      )}
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  );
}
