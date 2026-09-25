---
title: Linked identities
description: Connect Google, GitHub, OpenID Connect or SAML sign-in to your Lahijan account, see what is linked and unlink it.
---

A linked identity lets you sign in to Lahijan with an account you already have somewhere else: Google, GitHub, your company's OpenID Connect (OIDC) provider or a SAML 2.0 identity provider. You see and remove linked identities on **Settings > Identities**.

Which providers you can use depends on what your operator has turned on. Each configured provider has a short key, such as `google`, `github`, `keycloak` or `entra`; ask your operator for the keys available on your server.

## What the list shows

**Settings > Identities** (titled **Linked identities**) lists every external identity connected to your account:

| Column       | Meaning                                                                                                                     |
| ------------ | --------------------------------------------------------------------------------------------------------------------------- |
| **Provider** | `google` or `github` for the built-in OAuth providers, `oidc:<key>` for an OIDC provider, `saml:<key>` for a SAML provider. |
| **Subject**  | The stable id the provider uses for you (the `sub` claim for OAuth and OIDC, the NameID for SAML).                          |
| **Scopes**   | What the provider granted when you last signed in. Empty for SAML.                                                          |
| **Linked**   | When the identity was connected.                                                                                            |

Lahijan never shows the provider's access or refresh tokens. They are stored encrypted and are not returned by any endpoint.

The same data is available from the API:

```sh
curl https://cloud.example.com/api/v1/me/identities \
  -H "Authorization: Bearer $LAHIJAN_TOKEN"
```

For SAML identities the response also includes `attributes`, the attribute values your identity provider sent at your last sign-in.

## Link a new identity

The dashboard has no "link" button yet. Linking happens when you start a provider's sign-in flow in a browser where you are already signed in to Lahijan:

1. Sign in to the dashboard.
2. In the same browser, open the start address for the provider:

   | Provider type              | Address                                               |
   | -------------------------- | ----------------------------------------------------- |
   | OAuth (`google`, `github`) | `https://<your server>/api/v1/auth/oauth/<key>/start` |
   | OpenID Connect             | `https://<your server>/api/v1/auth/oidc/<key>/start`  |
   | SAML 2.0                   | `https://<your server>/api/v1/auth/saml/<key>/start`  |

3. Sign in at the provider and approve the request.
4. You return to the dashboard. The new identity appears on **Settings > Identities**.

The flow must finish within 10 minutes of starting it.

What can go wrong:

- **The identity is already linked to another Lahijan account.** Linking is refused with `409` so nobody can take over another person's account. Unlink it from the other account first.
- **The provider key is unknown or disabled.** The start address answers `404`, or `501` if that kind of sign-in is not enabled at all.
- **The sign-in took too long or cookies were blocked.** The return step fails with an "invalid state" error. Start again.

## Sign in with a linked identity

Once linked, opening the same start address while signed out logs you in as your Lahijan account. There are no provider buttons on the dashboard sign-in page yet, so use the address directly or a link your operator provides.

> [!WARNING]
> Signing in through an external provider does not ask for your Lahijan second factor (authenticator app or recovery code). Your provider's own security settings apply instead.

If you sign in with an OAuth or OIDC identity that is not linked to any account, Lahijan creates a new account from the provider's profile. That new account has no tenant membership, so it cannot create resources until an administrator adds it to a tenant. If the provider's email already belongs to an existing Lahijan account, sign-in fails; sign in with your password and link the identity instead.

For SAML, a sign-in with an unknown identity is refused unless your operator has turned on automatic account creation for SAML.

## Unlink an identity

On **Settings > Identities**, select **Unlink** on the row. With the API:

```sh
curl -X DELETE https://cloud.example.com/api/v1/me/identities/<identity id> \
  -H "Authorization: Bearer $LAHIJAN_TOKEN"
```

Lahijan refuses to remove your last way to sign in. If you have no password and this is your only linked identity, the request fails with `409`. Set a password first with a [password reset](/docs/account/security#reset-a-forgotten-password), or link another identity.

Unlinking takes effect immediately and is recorded in the audit log as `auth.idp.unlink`. Linking and sign-ins are recorded as `auth.idp.link` and `auth.idp.login`.

For operators, configuring the providers themselves is covered in [Sign-in providers and email](/docs/operations/authentication).
