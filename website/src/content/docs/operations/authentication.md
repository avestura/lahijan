---
title: Sign-in providers and email
description: Configure passwords, registration, OAuth, OIDC, SAML 2.0, passkeys, TOTP and outgoing email for Lahijan.
---

This page shows how an operator sets up the ways people sign in to Lahijan and the email it sends for verification and password resets. Each section gives the config keys, a YAML example and the matching environment variables. Users manage their own factors and linked identities from the dashboard; see [Sign-in security](/docs/account/security) and [Linked identities](/docs/account/identities).

## Where settings come from

Lahijan builds its configuration in layers. A later layer wins over an earlier one:

1. Defaults built into the binary (the file `internal/app/lahijan/conf/.lahijan.conf.default.yaml`).
2. An optional config file on disk named `.lahijan.conf.default.yaml`, searched in `/etc/lahijan`, then `$HOME/.lahijan`, then the working directory. The first one found is merged over the defaults. In the production image the working directory is `/etc/lahijan`.
3. Environment variables: `LAHIJAN_` plus the key in upper case with dots replaced by underscores. `smtp.appBaseURL` becomes `LAHIJAN_SMTP_APPBASEURL`. An empty variable is ignored.
4. Command-line flags named after the key, for example `--smtp.port=587`.

The production compose file already sets several auth and SMTP variables on the `lahijan` service (see [Environment file](/docs/operations/environment)). Because variables beat the config file, change those through `.env.prod` or a compose override, not the file.

To use a config file in the production stack, mount it with a compose override:

```yaml title="deployments/docker-compose.override.yml"
services:
  lahijan:
    volumes:
      - ./lahijan.yaml:/etc/lahijan/.lahijan.conf.default.yaml:ro
```

Then recreate the container:

```sh
docker compose --env-file deployments/.env.prod \
  -f deployments/docker-compose.prod.yml -f deployments/docker-compose.override.yml \
  up -d --force-recreate lahijan
```

> [!NOTE]
> Environment variables can only override keys that already exist in the built-in defaults. A new provider name (for example an OIDC provider called `authentik`) must be declared in the config file. After that its keys can also be set with environment variables.

## Local passwords and registration

| Key                                   | Default                             | Notes                                                                                                                                |
| ------------------------------------- | ----------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `auth.password.minLength`             | `12`                                | The production compose file sets `LAHIJAN_AUTH_PASSWORD_MINLENGTH=12`.                                                               |
| `auth.password.argon2.*`              | 64 MiB, 3 iterations, parallelism 2 | argon2id cost. Raising it slows every sign-in.                                                                                       |
| `auth.signup.personalTenant`          | `true`                              | Gives each self-registered user a personal tenant they own. When false, new accounts have no tenant until an admin adds them to one. |
| `auth.session.lifetimeSeconds`        | `86400`                             | Session lifetime.                                                                                                                    |
| `auth.session.refreshLifetimeSeconds` | `2592000`                           | Refresh token lifetime.                                                                                                              |
| `auth.session.secure`                 | `false`                             | Set to `true` by the production compose file (HTTPS-only cookies).                                                                   |

Registration through `POST /api/v1/auth/register` is always open: there is no setting that turns it off. Anyone who can reach the dashboard can create an account. Sign-in does not require a verified email address. Plan tenant access with `auth.signup.personalTenant` and the roles described in [Users and tenants](/docs/admin/users-and-tenants).

`auth.password.breachCheck.enabled` exists in the config file but has no effect in this release.

## OAuth (Google and GitHub)

Two OAuth presets exist: `google` and `github`. Any other name under `auth.oauth.providers` is built with Google's endpoints, so use OIDC for other identity providers.

```yaml title="/etc/lahijan/.lahijan.conf.default.yaml"
auth:
  oauth:
    redirectBase: "https://app.example.com"
    providers:
      github:
        enabled: true
        clientId: "Iv1.0123456789abcdef"
        clientSecret: "change-me"
```

The same settings as environment variables, set on the `lahijan` service in a compose override:

```yaml title="deployments/docker-compose.override.yml"
services:
  lahijan:
    environment:
      LAHIJAN_AUTH_OAUTH_REDIRECTBASE: "https://app.example.com"
      LAHIJAN_AUTH_OAUTH_PROVIDERS_GITHUB_ENABLED: "true"
      LAHIJAN_AUTH_OAUTH_PROVIDERS_GITHUB_CLIENTID: "Iv1.0123456789abcdef"
      LAHIJAN_AUTH_OAUTH_PROVIDERS_GITHUB_CLIENTSECRET: "change-me"
```

Register this callback URL with the provider:

```text
https://app.example.com/api/v1/auth/oauth/<provider>/callback
```

Set `redirectBase` to your public origin in production. When it is empty the callback URL is derived from the request. Default scopes are `openid email profile` for Google and `read:user user:email` for GitHub.

A sign-in with an external identity Lahijan has not seen before creates a new account. Signed-in users can also link an identity to their existing account.

## OpenID Connect

Any OIDC provider (Keycloak, Authentik, Okta, Auth0 and others) is configured under `auth.oidc.providers.<name>`. The default file ships a disabled `keycloak` entry.

```yaml title="/etc/lahijan/.lahijan.conf.default.yaml"
auth:
  oidc:
    redirectBase: "https://app.example.com"
    providers:
      keycloak:
        enabled: true
        issuer: "https://sso.example.com/realms/main"
        clientId: "lahijan"
        clientSecret: "change-me"
        scopes: ["openid", "email", "profile"]
```

The equivalent variables are `LAHIJAN_AUTH_OIDC_REDIRECTBASE`, `LAHIJAN_AUTH_OIDC_PROVIDERS_KEYCLOAK_ENABLED`, `LAHIJAN_AUTH_OIDC_PROVIDERS_KEYCLOAK_ISSUER`, `LAHIJAN_AUTH_OIDC_PROVIDERS_KEYCLOAK_CLIENTID` and `LAHIJAN_AUTH_OIDC_PROVIDERS_KEYCLOAK_CLIENTSECRET`.

The callback URL is `https://app.example.com/api/v1/auth/oidc/<provider>/callback`. Lahijan runs OIDC discovery against the issuer at startup. If the issuer cannot be reached or is wrong, Lahijan exits with `oidc discovery for <provider>` in the log.

Tokens returned by OAuth and OIDC providers are encrypted at rest with `auth.secrets.encryptionKey`.

## SAML 2.0

Lahijan acts as a SAML service provider. The default file ships a disabled `entra` entry; add more under `auth.saml.providers`.

### Service provider certificate

Outside a dev environment an enabled SAML provider requires a signing key and certificate, both PEM. Lahijan exits at startup if either is missing. Create a pair:

```sh
openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
  -keyout sp.key -out sp.crt -subj "/CN=app.example.com"
```

PKCS#1 and PKCS#8 keys are accepted. Because the values span many lines, a config file with block scalars is easier than environment variables (`LAHIJAN_AUTH_SAML_SPSIGNINGKEY`, `LAHIJAN_AUTH_SAML_SPSIGNINGCERT`). The `*_FILE` variants mentioned in comments in the default config are not implemented.

### Provider settings

```yaml title="/etc/lahijan/.lahijan.conf.default.yaml"
auth:
  saml:
    redirectBase: "https://app.example.com"
    spSigningKey: |
      -----BEGIN PRIVATE KEY-----
      ...
      -----END PRIVATE KEY-----
    spSigningCert: |
      -----BEGIN CERTIFICATE-----
      ...
      -----END CERTIFICATE-----
    jit:
      enabled: false
    providers:
      entra:
        enabled: true
        entityId: ""
        idpMetadataURL: "https://login.microsoftonline.com/<tenant>/federationmetadata/2007-06/federationmetadata.xml"
        allowIdpInitiated: false
        emailAttribute: ""
        nameAttribute: ""
```

| Key                                 | Meaning                                                                                      |
| ----------------------------------- | -------------------------------------------------------------------------------------------- |
| `redirectBase`                      | Public origin used to build the SP URLs.                                                     |
| `entityId`                          | SP entity ID. Empty means the metadata URL.                                                  |
| `idpMetadataXML` / `idpMetadataURL` | IdP metadata inline or fetched once at startup. Inline XML wins.                             |
| `allowIdpInitiated`                 | Accept responses without `InResponseTo`. Leave off unless you need IdP-initiated sign-in.    |
| `emailAttribute`, `nameAttribute`   | Attribute names for email and display name. Empty uses the standard claim URIs.              |
| `jit.enabled`                       | Create an account when an unknown SAML user signs in. When false, unknown users are refused. |

Give your IdP these values:

| Value                      | URL                                                       |
| -------------------------- | --------------------------------------------------------- |
| SP metadata                | `https://app.example.com/api/v1/auth/saml/metadata`       |
| Assertion consumer service | `https://app.example.com/api/v1/auth/saml/<provider>/acs` |

## Two-factor authentication

TOTP and recovery codes work without extra setup.

| Key                          | Default   | Notes                                               |
| ---------------------------- | --------- | --------------------------------------------------- |
| `auth.mfa.totp.issuer`       | `Lahijan` | Name shown in authenticator apps.                   |
| `auth.mfa.recovery.count`    | `10`      | Recovery codes per batch.                           |
| `auth.mfa.pendingTTLSeconds` | `300`     | Time a user has to complete the second step.        |
| `auth.mfa.maxAttempts`       | `5`       | Failed codes before the pending sign-in is revoked. |

### Passkeys (WebAuthn)

WebAuthn is turned on only when both a relying party ID and at least one origin are set:

```ini title="deployments/.env.prod"
LAHIJAN_AUTH_MFA_WEBAUTHN_RPID=app.example.com
LAHIJAN_AUTH_MFA_WEBAUTHN_RPORIGINS=https://app.example.com
```

`rpId` must be the dashboard host or a registrable parent of it. `rpOrigins` is a space-separated list of full origins. `auth.mfa.webauthn.rpDisplayName` (default `Lahijan`) is shown in the browser prompt. Without these, the passkey endpoints answer "feature disabled" and TOTP keeps working.

## Email and SMTP

Lahijan sends email for address verification (on registration and on request), password resets and email-address changes. Links are built from `smtp.appBaseURL`, which the production compose file sets from `LAHIJAN_PUBLIC_URL`.

| Key               | Default                 | Production variable                             |
| ----------------- | ----------------------- | ----------------------------------------------- |
| `smtp.enabled`    | `false`                 | `LAHIJAN_SMTP_ENABLED` (compose default `true`) |
| `smtp.host`       | `localhost`             | `LAHIJAN_SMTP_HOST`                             |
| `smtp.port`       | `1025`                  | `LAHIJAN_SMTP_PORT` (compose default `587`)     |
| `smtp.user`       | empty                   | `LAHIJAN_SMTP_USER`                             |
| `smtp.password`   | empty                   | `LAHIJAN_SMTP_PASSWORD`                         |
| `smtp.from`       | `noreply@localhost`     | `LAHIJAN_SMTP_FROM`                             |
| `smtp.fromName`   | `Lahijan`               | not set by compose                              |
| `smtp.appBaseURL` | `http://localhost:3000` | `LAHIJAN_PUBLIC_URL`                            |

Link lifetimes are `auth.email.verificationTTLSeconds` (1 day), `auth.email.passwordResetTTLSeconds` (1 hour) and `auth.email.emailChangeTTLSeconds` (1 day).

Things to know about delivery:

- Mail is sent with Go's `net/smtp`. It upgrades to TLS with STARTTLS when the server offers it, and uses PLAIN authentication when `smtp.user` is set. Use a submission port with STARTTLS, such as 587. Implicit TLS on port 465 is not supported.
- PLAIN authentication is refused over an unencrypted connection unless the server is `localhost`.
- When `smtp.enabled` is false, messages are dropped without an error.
- A failed verification email does not block registration.
