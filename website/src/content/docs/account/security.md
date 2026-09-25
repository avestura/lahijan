---
title: Sign-in security
description: Password rules, email verification, password reset, authenticator apps, recovery codes, passkeys and signing in with a second factor.
---

This page covers how you prove who you are to Lahijan: your password, the emails that verify your address or reset your password, and the second factors you can add on **Settings > Security**. Read the [limits](#current-limits) before you turn on a second factor: in this release the dashboard sign-in form cannot complete a second-factor challenge.

## Password rules

Lahijan checks every new password on the server when you register, change your password or reset it.

- At least 12 characters. Your operator can raise or lower this with `auth.password.minLength` for registration and password changes; password resets always require at least 12.
- At least three of these four character types: lowercase letters, uppercase letters, digits, symbols.

A password that fails these rules is rejected with `400` and a message saying why. The dashboard's placeholder text says "At least 8 characters", but the server rule above is what applies.

Passwords are stored only as argon2id hashes. Nobody, including your operator, can read your password back.

## Verify your email address

When you register, Lahijan emails a verification link to your address. The link is valid for 24 hours (the operator setting `auth.email.verificationTTLSeconds`). Emails are only sent if your operator has configured outgoing mail.

The link has the form `https://<dashboard>/verify-email?token=<token>`. The dashboard has no page for this path yet, so confirm the token with the API:

```sh
curl -X POST https://cloud.example.com/api/v1/auth/verify-email \
  -H "Content-Type: application/json" \
  -d '{"token": "<token from the link>"}'
```

To get a new link, call `POST /api/v1/auth/resend-verification` with your email. The response is the same whether or not the address has an account, so it cannot be used to discover accounts.

```sh
curl -X POST https://cloud.example.com/api/v1/auth/resend-verification \
  -H "Content-Type: application/json" \
  -d '{"email": "you@example.com"}'
```

A new link replaces any earlier one. Signing in does not currently require a verified email.

## Reset a forgotten password

1. Request a reset link. The response is always the same, whether or not the email has an account.

   ```sh
   curl -X POST https://cloud.example.com/api/v1/auth/password-reset/request \
     -H "Content-Type: application/json" \
     -d '{"email": "you@example.com"}'
   ```

2. Open the email. The link has the form `https://<dashboard>/reset-password?token=<token>` and is valid for one hour (`auth.email.passwordResetTTLSeconds`).
3. Set the new password with the token from the link:

   ```sh
   curl -X POST https://cloud.example.com/api/v1/auth/password-reset/confirm \
     -H "Content-Type: application/json" \
     -d '{"token": "<token from the link>", "newPassword": "a-Longer-passphrase-42"}'
   ```

The dashboard has no forgot-password page yet, so these steps use the API. Each link works once. A reset does not sign out your existing sessions.

## Authenticator app (TOTP)

An authenticator app (for example Aegis, Google Authenticator or 1Password) generates a new 6-digit code every 30 seconds. Once it is set up, signing in needs your password and a current code.

1. Open **Settings > Security**.
2. On the **Authenticator app** card, select **Set up**.
3. Scan the QR code with your app. If you cannot scan it, type the secret shown under "Can't scan? Enter this secret manually:".
4. Type the current code into **6-digit code** and select **Confirm**.
5. Lahijan shows a list of **Recovery codes**. Save them now; they are shown only once.

The app is listed under the issuer name your operator configured (`auth.mfa.totp.issuer`, "Lahijan" by default) and your email address.

To turn it off, select **Disable** and enter your **Current password**. You cannot set up a second authenticator over an active one; disable the old one first.

With the API, the same steps are `POST /api/v1/me/mfa/totp/enroll` (returns `secret` and `provisioningUri`), `POST /api/v1/me/mfa/totp/verify` with `{"code": "123456"}` (returns the recovery codes), and `POST /api/v1/me/mfa/totp/disable` with `{"currentPassword": "..."}`.

## Recovery codes

Recovery codes let you finish signing in when you do not have your authenticator. You get 10 codes (the operator setting `auth.mfa.recovery.count`). Each code works once.

- The **Recovery codes** card shows how many are left, for example "7 of 10 remaining".
- **Regenerate codes** creates a new set and invalidates every old code. The new codes are shown once.

The API equivalents are `GET /api/v1/me/mfa/recovery` (counts only; codes are never returned again) and `POST /api/v1/me/mfa/recovery` (new set).

> [!NOTE]
> The **Authenticator app** card decides whether to show "Enabled" by checking whether you have recovery codes. After you disable the authenticator, or if you generated codes without one, the badge can be wrong.

## Passkeys and security keys (WebAuthn)

A passkey uses your device's built-in authenticator (Touch ID, Windows Hello) or a hardware security key.

1. Open **Settings > Security**.
2. On **Passkeys / security keys**, select **Add a passkey**.
3. Enter a **Label** (for example "Work laptop") and select **Save**.
4. Follow your browser's prompt.

Passkeys only work when your operator has configured the WebAuthn settings for the dashboard's domain; otherwise the server answers "feature disabled". If your browser has no WebAuthn support, the card says "Your browser does not support WebAuthn."

The card does not list the passkeys you have added, and removing a passkey (`DELETE /api/v1/me/mfa/webauthn/credentials/{credentialId}`) is not implemented yet.

## Sign in with a second factor

Once you have an authenticator app or a passkey, `POST /api/v1/auth/login` no longer opens a session right away. It returns `202` with a short-lived token:

```json
{
	"mfaRequired": true,
	"pendingSessionToken": "<token>",
	"enrolledFactors": ["totp"]
}
```

Finish signing in by sending a code within five minutes (`auth.mfa.pendingTTLSeconds`):

```sh
curl -X POST https://cloud.example.com/api/v1/auth/mfa/challenge \
  -H "Content-Type: application/json" \
  -d '{"pendingSessionToken": "<token>", "kind": "totp", "code": "123456"}'
```

Use `"kind": "recovery"` with one of your recovery codes instead of a TOTP code. After five wrong attempts (`auth.mfa.maxAttempts`) the pending token is revoked and you start again with your password.

A tenant can also be marked as requiring a second factor. If you belong to such a tenant and have no factor set up, sign-in is refused with `403`.

## Current limits

> [!CAUTION]
> The dashboard sign-in form does not support the second-factor step. If you turn on an authenticator app or add a passkey, the form stops at "Multi-factor authentication required" and you cannot sign in to the dashboard. You can still use the API with [access tokens](/docs/account/access-tokens) created beforehand.

> [!CAUTION]
> Passkeys cannot be used to answer the sign-in challenge in this release. If a passkey is your only factor, you can only finish signing in with a recovery code. Recovery codes are created automatically only when you confirm an authenticator app, so generate them with **Regenerate codes** before you add a passkey.

Other limits:

- Changing or resetting your password does not end other sessions.
- Registration, email verification and password reset have no dashboard pages; use the API calls shown above.
