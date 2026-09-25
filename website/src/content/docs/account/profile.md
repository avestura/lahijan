---
title: Profile
description: Change your display name, preferred language and password, and see what an email change does today.
---

Your profile holds the details other people and the dashboard see about you: your email address, a display name and a preferred language. You manage it on **Settings > Profile** or through the API.

## What you can change

| Field              | Dashboard            | API          | Notes                                              |
| ------------------ | -------------------- | ------------ | -------------------------------------------------- |
| Display name       | Yes                  | Yes          | Takes effect immediately.                          |
| Preferred language | Yes                  | Yes          | `en` (English) or `fa` (Persian) in the dashboard. |
| Password           | Yes                  | Yes          | Needs your current password.                       |
| Email              | No (shown read-only) | Request only | See [Changing your email](#changing-your-email).   |

## Edit your profile in the dashboard

1. Open **Settings > Profile**.
2. Change **Display name** or **Preferred language**.
3. Select **Save changes**.

The **Email** field is shown but cannot be edited here.

> [!NOTE]
> The form only sends **Preferred language** when it is set to something other than English. To switch a saved preference back to English, use the API (`"locale": "en"`).

## Change your password

1. Open **Settings > Profile**.
2. Under **Change password**, fill in **Current password**, **New password** and **Confirm new password**.
3. Select **Update password**.

The server checks the new password against the password rules described in [Sign-in security](/docs/account/security#password-rules). If your current password is wrong, the change is rejected and nothing is saved.

> [!NOTE]
> Changing your password does not sign you out of other browsers or devices. Their sessions stay valid until they expire. See [Sessions](/docs/account/sessions).

If you signed up through an external provider and never set a password, you cannot use this form: there is no current password to confirm. Use a [password reset](/docs/account/security#reset-a-forgotten-password) to set one.

## Update your profile with the API

Read your profile with `GET /api/v1/auth/me` and change it with `PATCH /api/v1/auth/me`. Send only the fields you want to change.

```sh
curl https://cloud.example.com/api/v1/auth/me \
  -H "Authorization: Bearer $LAHIJAN_TOKEN"
```

The response contains your `id`, `email`, `displayName` and a `memberships` list. Each membership has a `tenantId` and the `role` you hold in that tenant. You need a `tenantId` for most other API calls (see [Access tokens](/docs/account/access-tokens#tenant-header)).

```sh
curl -X PATCH https://cloud.example.com/api/v1/auth/me \
  -H "Authorization: Bearer $LAHIJAN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"displayName": "Sara Ahmadi", "locale": "fa"}'
```

To change the password, send both `currentPassword` and `newPassword`. If `newPassword` is present without `currentPassword`, the request fails with `400`.

```json
{
	"currentPassword": "old-Passw0rd#1",
	"newPassword": "a-Longer-passphrase-42"
}
```

| Field             | Type           | Meaning                               |
| ----------------- | -------------- | ------------------------------------- |
| `displayName`     | string or null | Your display name.                    |
| `locale`          | string         | Preferred language code.              |
| `currentPassword` | string         | Required together with `newPassword`. |
| `newPassword`     | string         | The new password.                     |
| `newEmail`        | string         | Starts an email change.               |

## Changing your email

Sending `newEmail` to `PATCH /api/v1/auth/me` emails a confirmation link to the new address. Your account email does not change until that link is used.

> [!WARNING]
> In the current release there is no endpoint or dashboard page that accepts the email-change link, so the change cannot be completed. Your account keeps its original email. If you need a different email address, ask your operator.

## Language

The preferred language you save is stored on your account and is used for the emails Lahijan sends you (verification and password reset). The dashboard also has its own language switch in the header, which changes the interface language for the current browser. Error messages returned by the API follow the language your browser or client sends in the `Accept-Language` header.
