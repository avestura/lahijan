/**
 * WebAuthnCard — register + manage passkeys / security keys.
 *
 * Uses the browser's navigator.credentials WebAuthn API. If the
 * browser doesn't support WebAuthn we hide the "Add" button and show
 * the localized "browser does not support" notice.
 */
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { KeyIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { EmptyState } from "@/components/layout/EmptyState";
import { useAddWebAuthnCredential } from "../api";

export function WebAuthnCard() {
  const { t } = useTranslation();
  const add = useAddWebAuthnCredential();
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");

  const isSupported =
    typeof window !== "undefined" && typeof window.PublicKeyCredential !== "undefined";

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    add.mutate(
      { name: name.trim() },
      {
        onSettled: () => {
          setOpen(false);
          setName("");
        },
      },
    );
  };

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base">{t("settings.security.webauthn.title")}</CardTitle>
        {isSupported && (
          <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
            {t("settings.security.webauthn.add")}
          </Button>
        )}
      </CardHeader>
      <CardContent>
        {!isSupported ? (
          <p className="text-sm text-muted-foreground">
            {t("settings.security.webauthn.supported")}
          </p>
        ) : (
          <EmptyState
            icon={KeyIcon}
            title={t("settings.security.webauthn.empty")}
            description={t("settings.security.webauthn.emptyBody")}
          />
        )}
      </CardContent>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("settings.security.webauthn.add")}</DialogTitle>
            <DialogDescription>{t("settings.security.webauthn.empty")}</DialogDescription>
          </DialogHeader>
          <form onSubmit={onSubmit} className="space-y-3">
            <div className="space-y-2">
              <Label htmlFor="webauthn-name">{t("settings.security.webauthn.name.label")}</Label>
              <Input
                id="webauthn-name"
                placeholder={t("settings.security.webauthn.name.placeholder")}
                value={name}
                onChange={(e) => setName(e.target.value)}
              />
            </div>
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setOpen(false)}>
                {t("common.cancel")}
              </Button>
              <Button type="submit" disabled={add.isPending || !name.trim()}>
                {t("common.save")}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
