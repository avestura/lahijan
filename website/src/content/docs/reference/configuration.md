---
title: Configuration reference
description: Every Lahijan server setting with its default, environment variable and command-line flag.
---

The Lahijan server reads its settings from four layers. Each layer overrides the ones below it:

1. **Command-line flags**, one per setting: `--http.server.port=8080`.
2. **Environment variables** named `LAHIJAN_` plus the setting path in capitals with dots replaced by underscores: `LAHIJAN_HTTP_SERVER_PORT=8080`.
3. **A config file** named `.lahijan.conf.default.yaml`, looked up in `/etc/lahijan`, then `$HOME/.lahijan`, then the working directory. The first one found is merged over the built-in defaults. It only needs the settings you change.
4. **Built-in defaults**, shown in the tables below.

Setting names are case-sensitive in flags (`--database.maxConns`) and case-insensitive in the environment (`LAHIJAN_DATABASE_MAXCONNS`).

```yaml title="/etc/lahijan/.lahijan.conf.default.yaml"
http:
  server:
    port: 8080
database:
  host: db.internal
  sslmode: verify-full
```

> [!NOTE]
> List settings take comma-separated values as flags (`--http.server.cors.allowOrigins=https://a.example.com,https://b.example.com`) and space-separated values as environment variables.

> [!WARNING]
> Put secrets such as `database.password`, `auth.signing.key` and provider API keys in environment variables or a file readable only by the server, never in a file you commit. In the production compose stack they come from `deployments/.env.prod`. See [Environment file](/docs/operations/environment).

## General

Two top-level keys: `environment` decides whether Lahijan accepts development fallbacks for missing secrets, and `debug` raises the log level.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `environment` | `LAHIJAN_ENVIRONMENT` | string | `dev` | Name of the deployment environment, such as `dev`, `stg` or `prd`. The values `dev`, `development`, `local` and an empty string (any case) count as development, where Lahijan fills in insecure stand-ins for missing secrets. In any other environment, `auth.signing.key`, `auth.secrets.encryptionKey` and (with SAML) the SAML signing key and certificate must be set or the server refuses to start. |
| `debug` | `LAHIJAN_DEBUG` | boolean | `false` | Turns on trace-level logging and extra startup messages. Leave it off in production; it makes the logs much larger. |

## HTTP server

The HTTP server that serves the Lahijan API and the health check endpoints: listen address, request limits, CORS, request logging and probes. See [Observability](/docs/operations/observability) for logs and [TLS and domains](/docs/operations/tls-and-domains) for running behind a reverse proxy.

### `http.server`

Listen address, connection limits and request size for the HTTP server.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `http.server.port` | `LAHIJAN_HTTP_SERVER_PORT` | integer | `3000` | TCP port the HTTP server listens on. |
| `http.server.host` | `LAHIJAN_HTTP_SERVER_HOST` | string | `0.0.0.0` | Address the HTTP server binds to. `0.0.0.0` listens on every IPv4 interface; `127.0.0.1` accepts local connections only, for example when a reverse proxy runs on the same host. |
| `http.server.concurrency` | `LAHIJAN_HTTP_SERVER_CONCURRENCY` | integer | `262144` (computed) | Maximum number of concurrent connections. When not set, it is computed at startup from the Fiber web framework default of 262144 connections. |
| `http.server.bodylimit` | `LAHIJAN_HTTP_SERVER_BODYLIMIT` | integer | `4194304` (computed) | Maximum request body size, in bytes. Larger requests are rejected. When not set, it is computed at startup from the Fiber web framework default of 4194304 bytes (4 MiB). Raise it if administrators upload plugin modules bigger than 4 MiB, since this limit applies before `wasm.maxModuleSize`. |
| `http.server.prefork` | `LAHIJAN_HTTP_SERVER_PREFORK` | boolean | `false` | Starts one server process per CPU, all sharing the same port. Each child process runs the full server, including background workers, so leave it off unless you have tested it for your setup. |
| `http.server.cors.enabled` | `LAHIJAN_HTTP_SERVER_CORS_ENABLED` | boolean | `false` | Adds CORS headers to responses. Turn it on only if a browser app served from another origin calls the API directly. |
| `http.server.cors.allowHeaders` | `LAHIJAN_HTTP_SERVER_CORS_ALLOWHEADERS` | list of strings | empty list | Request headers that cross-origin requests may send, such as `Content-Type` or `Authorization`. |
| `http.server.cors.allowMethods` | `LAHIJAN_HTTP_SERVER_CORS_ALLOWMETHODS` | list of strings | empty list | HTTP methods that cross-origin requests may use, such as `GET` or `POST`. |
| `http.server.cors.allowOrigins` | `LAHIJAN_HTTP_SERVER_CORS_ALLOWORIGINS` | list of strings | empty list | Origins allowed to call the API from the browser, written as scheme and host, for example `https://app.example.com`. |
| `http.server.cors.maxAge` | `LAHIJAN_HTTP_SERVER_CORS_MAXAGE` | integer | `0` | How long browsers may cache a CORS preflight response, in seconds. `0` sends no max-age, so the browser uses its own default. |
| `http.server.logger.enabled` | `LAHIJAN_HTTP_SERVER_LOGGER_ENABLED` | boolean | `true` | Writes one log line for every HTTP request. |
| `http.server.logger.colors` | `LAHIJAN_HTTP_SERVER_LOGGER_COLORS` | boolean | `true` | Intended to toggle colored request logs. The current server does not read this key. |
| `http.server.healthcheck.enabled` | `LAHIJAN_HTTP_SERVER_HEALTHCHECK_ENABLED` | boolean | `true` | Serves the liveness and readiness endpoints. |
| `http.server.healthcheck.readinessEndpoint` | `LAHIJAN_HTTP_SERVER_HEALTHCHECK_READINESSENDPOINT` | string | `/healthcheck/readiness` | Path of the readiness endpoint. |
| `http.server.healthcheck.livenessEndpoint` | `LAHIJAN_HTTP_SERVER_HEALTHCHECK_LIVENESSENDPOINT` | string | `/healthcheck/liveness` | Path of the liveness endpoint. |

## Database

Connection to the PostgreSQL database that holds all Lahijan data, including the River job queue. Lahijan never migrates the schema on its own; see [Upgrades](/docs/operations/upgrades).

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `database.host` | `LAHIJAN_DATABASE_HOST` | string | `localhost` | Host name or IP address of the PostgreSQL server. |
| `database.port` | `LAHIJAN_DATABASE_PORT` | integer | `5432` | TCP port of the PostgreSQL server. |
| `database.name` | `LAHIJAN_DATABASE_NAME` | string | `lahijan` | Name of the PostgreSQL database Lahijan uses. |
| `database.user` | `LAHIJAN_DATABASE_USER` | string | `lahijan` | PostgreSQL user Lahijan connects as. |
| `database.password` | `LAHIJAN_DATABASE_PASSWORD` | string | empty | Password of the PostgreSQL user. Set it through the environment variable, never commit it. |
| `database.sslmode` | `LAHIJAN_DATABASE_SSLMODE` | string | `disable` | TLS mode for the database connection: `disable`, `require`, `verify-ca` or `verify-full`. Use `verify-full` when PostgreSQL runs on another host. |
| `database.maxConns` | `LAHIJAN_DATABASE_MAXCONNS` | integer | `20` | Maximum number of connections in the pool. |
| `database.minConns` | `LAHIJAN_DATABASE_MINCONNS` | integer | `2` | Number of connections the pool keeps open even when idle. |
| `database.maxConnLifetimeSeconds` | `LAHIJAN_DATABASE_MAXCONNLIFETIMESECONDS` | integer | `3600` | Maximum age of a pooled connection, in seconds. Older connections are closed and replaced. |
| `database.maxConnIdleSeconds` | `LAHIJAN_DATABASE_MAXCONNIDLESECONDS` | integer | `300` | How long an unused connection may stay open, in seconds, before it is closed. |
| `database.statementTimeoutMs` | `LAHIJAN_DATABASE_STATEMENTTIMEOUTMS` | integer | `5000` | Maximum run time of a single SQL statement, in milliseconds. PostgreSQL cancels statements that run longer. `0` turns the limit off. |

## Authentication

Sign-up, password hashing, browser sessions, access tokens, email links, single sign-on (OAuth, OpenID Connect, SAML) and multi-factor authentication. [Authentication](/docs/operations/authentication) walks through setting up each identity provider.

### `auth.signup`

What happens when a user registers on their own.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `auth.signup.personalTenant` | `LAHIJAN_AUTH_SIGNUP_PERSONALTENANT` | boolean | `true` | Gives every user who signs up a personal tenant that they own. Without a tenant, a new account can open the dashboard but cannot create anything. Turn it off if you add users to tenants yourself. See [Users and tenants](/docs/admin/users-and-tenants). |

### `auth.password`

Password hashing parameters and the password policy.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `auth.password.argon2.memory` | `LAHIJAN_AUTH_PASSWORD_ARGON2_MEMORY` | integer | `65536` | Memory used to hash one password with argon2id, in KiB (65536 KiB is 64 MiB). Higher values make stolen hashes harder to crack but make every sign-in use more memory. Existing hashes keep working after a change. |
| `auth.password.argon2.iterations` | `LAHIJAN_AUTH_PASSWORD_ARGON2_ITERATIONS` | integer | `3` | Number of argon2id passes (time cost). Higher is slower and harder to crack. |
| `auth.password.argon2.parallelism` | `LAHIJAN_AUTH_PASSWORD_ARGON2_PARALLELISM` | integer | `2` | Number of threads argon2id uses for one hash. |
| `auth.password.argon2.saltLength` | `LAHIJAN_AUTH_PASSWORD_ARGON2_SALTLENGTH` | integer | `16` | Length of the random salt stored with each password hash, in bytes. |
| `auth.password.argon2.keyLength` | `LAHIJAN_AUTH_PASSWORD_ARGON2_KEYLENGTH` | integer | `32` | Length of the derived hash, in bytes. |
| `auth.password.minLength` | `LAHIJAN_AUTH_PASSWORD_MINLENGTH` | integer | `12` | Minimum number of characters in a new password. Passwords must also contain at least three of: lowercase letters, uppercase letters, digits and symbols. |
| `auth.password.breachCheck.enabled` | `LAHIJAN_AUTH_PASSWORD_BREACHCHECK_ENABLED` | boolean | `false` | Intended to check new passwords against the Have I Been Pwned breach list. Not implemented yet; the current server does not read this key. |

### `auth.session`

Browser session and refresh cookies.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `auth.session.cookieName` | `LAHIJAN_AUTH_SESSION_COOKIENAME` | string | `lahijan_session` | Name of the cookie that holds the browser session. |
| `auth.session.refreshCookieName` | `LAHIJAN_AUTH_SESSION_REFRESHCOOKIENAME` | string | `lahijan_refresh` | Name of the cookie that holds the refresh token, which renews the session. |
| `auth.session.domain` | `LAHIJAN_AUTH_SESSION_DOMAIN` | string | empty | Domain attribute of the session cookies. Empty means the cookies belong to the exact host that served them. Set a parent domain only if several subdomains must share the session. |
| `auth.session.secure` | `LAHIJAN_AUTH_SESSION_SECURE` | boolean | `false` | Marks the cookies as HTTPS-only. Set it to true in production; browsers then refuse to send the cookies over plain HTTP. |
| `auth.session.sameSite` | `LAHIJAN_AUTH_SESSION_SAMESITE` | string | `lax` | SameSite attribute of the cookies: `strict`, `lax` or `none`. `lax` suits most setups. `none` requires `secure` to be true. |
| `auth.session.path` | `LAHIJAN_AUTH_SESSION_PATH` | string | `/` | URL path the cookies apply to. |
| `auth.session.lifetimeSeconds` | `LAHIJAN_AUTH_SESSION_LIFETIMESECONDS` | integer | `86400` | Session lifetime, in seconds. The window slides forward while the user is active. |
| `auth.session.refreshLifetimeSeconds` | `LAHIJAN_AUTH_SESSION_REFRESHLIFETIMESECONDS` | integer | `2592000` | Lifetime of the refresh token, in seconds. After it expires the user must sign in again. |

### `auth.pat`

Personal access tokens that users create for API access.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `auth.pat.prefix` | `LAHIJAN_AUTH_PAT_PREFIX` | string | `lah_pat_` | Text placed at the start of every personal access token so users and secret scanners can recognize it. |
| `auth.pat.byteLength` | `LAHIJAN_AUTH_PAT_BYTELENGTH` | integer | `32` | Number of random bytes in each personal access token, before encoding. |

### `auth.email`

Single-use links sent by email for verification, password reset and email change.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `auth.email.verificationTTLSeconds` | `LAHIJAN_AUTH_EMAIL_VERIFICATIONTTLSECONDS` | integer | `86400` | How long an email verification link stays valid, in seconds. |
| `auth.email.passwordResetTTLSeconds` | `LAHIJAN_AUTH_EMAIL_PASSWORDRESETTTLSECONDS` | integer | `3600` | How long a password reset link stays valid, in seconds. |
| `auth.email.emailChangeTTLSeconds` | `LAHIJAN_AUTH_EMAIL_EMAILCHANGETTLSECONDS` | integer | `86400` | How long the confirmation link for an email address change stays valid, in seconds. |
| `auth.email.byteLength` | `LAHIJAN_AUTH_EMAIL_BYTELENGTH` | integer | `32` | Number of random bytes in each email link token. |

### `auth.signing`

Key that signs session cookies and email links.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `auth.signing.key` | `LAHIJAN_AUTH_SIGNING_KEY` | string | empty | Secret key that signs session cookies and email links. Required outside development: the server refuses to start without it. Use a long random value. Changing it signs every user out and invalidates links already sent. Set it through the environment variable, never commit it. |

### `auth.secrets`

Key that encrypts tokens received from external identity providers.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `auth.secrets.encryptionKey` | `LAHIJAN_AUTH_SECRETS_ENCRYPTIONKEY` | string | empty | Key that encrypts tokens from external identity providers before they are stored: 32 random bytes, base64-encoded (for example the output of `openssl rand -base64 32`). Required outside development. Changing it makes tokens already stored unreadable. Set it through the environment variable, never commit it. |

### `auth.oauth`

Sign-in with Google and GitHub through OAuth 2.0. Each provider is keyed by a name that appears in its URLs. The flow always uses PKCE.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `auth.oauth.providers.google.enabled` | `LAHIJAN_AUTH_OAUTH_PROVIDERS_GOOGLE_ENABLED` | boolean | `false` | Turns on sign-in with Google. |
| `auth.oauth.providers.google.clientId` | `LAHIJAN_AUTH_OAUTH_PROVIDERS_GOOGLE_CLIENTID` | string | empty | OAuth client ID from the Google Cloud console. |
| `auth.oauth.providers.google.clientSecret` | `LAHIJAN_AUTH_OAUTH_PROVIDERS_GOOGLE_CLIENTSECRET` | string | empty | OAuth client secret from the Google Cloud console. Set it through the environment variable, never commit it. |
| `auth.oauth.providers.google.scopes` | `LAHIJAN_AUTH_OAUTH_PROVIDERS_GOOGLE_SCOPES` | list of strings | `openid`, `email`, `profile` | OAuth scopes requested from Google. `openid`, `email` and `profile` are the minimum needed to link accounts. |
| `auth.oauth.providers.github.enabled` | `LAHIJAN_AUTH_OAUTH_PROVIDERS_GITHUB_ENABLED` | boolean | `false` | Turns on sign-in with GitHub. |
| `auth.oauth.providers.github.clientId` | `LAHIJAN_AUTH_OAUTH_PROVIDERS_GITHUB_CLIENTID` | string | empty | Client ID of the GitHub OAuth app. |
| `auth.oauth.providers.github.clientSecret` | `LAHIJAN_AUTH_OAUTH_PROVIDERS_GITHUB_CLIENTSECRET` | string | empty | Client secret of the GitHub OAuth app. Set it through the environment variable, never commit it. |
| `auth.oauth.providers.github.scopes` | `LAHIJAN_AUTH_OAUTH_PROVIDERS_GITHUB_SCOPES` | list of strings | `read:user`, `user:email` | OAuth scopes requested from GitHub. |
| `auth.oauth.redirectBase` | `LAHIJAN_AUTH_OAUTH_REDIRECTBASE` | string | empty | Public base URL of Lahijan, for example `https://app.example.com`. The callback URL is this base plus `/api/v1/auth/oauth/<provider>/callback`; register that URL with the provider. Empty derives the base from the request's host, which only works for local development. |

### `auth.oidc`

Sign-in with any OpenID Connect identity provider, such as Keycloak, Auth0 or Okta. The key under `providers` (here `keycloak`) is the name used in the provider's URLs.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `auth.oidc.providers.keycloak.enabled` | `LAHIJAN_AUTH_OIDC_PROVIDERS_KEYCLOAK_ENABLED` | boolean | `false` | Turns on sign-in with this OpenID Connect provider. |
| `auth.oidc.providers.keycloak.issuer` | `LAHIJAN_AUTH_OIDC_PROVIDERS_KEYCLOAK_ISSUER` | string | empty | Issuer URL of the provider, for example `https://sso.example.com`. Lahijan reads the provider's discovery document from it. |
| `auth.oidc.providers.keycloak.clientId` | `LAHIJAN_AUTH_OIDC_PROVIDERS_KEYCLOAK_CLIENTID` | string | empty | Client ID registered with the provider. |
| `auth.oidc.providers.keycloak.clientSecret` | `LAHIJAN_AUTH_OIDC_PROVIDERS_KEYCLOAK_CLIENTSECRET` | string | empty | Client secret registered with the provider. Set it through the environment variable, never commit it. |
| `auth.oidc.providers.keycloak.scopes` | `LAHIJAN_AUTH_OIDC_PROVIDERS_KEYCLOAK_SCOPES` | list of strings | `openid`, `email`, `profile` | OpenID Connect scopes requested at sign-in. |
| `auth.oidc.redirectBase` | `LAHIJAN_AUTH_OIDC_REDIRECTBASE` | string | empty | Public base URL of Lahijan. The callback URL is this base plus `/api/v1/auth/oidc/<provider>/callback`. Empty derives it from the request's host, which only works for local development. |

### `auth.saml`

SAML 2.0 single sign-on, with Lahijan as the service provider. Works with Microsoft Entra ID, Okta, OneLogin, Shibboleth and other SAML identity providers. The key under `providers` (here `entra`) is the name used in the provider's URLs.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `auth.saml.spSigningKey` | `LAHIJAN_AUTH_SAML_SPSIGNINGKEY` | string | empty | PEM-encoded RSA private key that Lahijan uses to sign its SAML requests and metadata. Required outside development; in development an empty value makes Lahijan generate a new key pair at every start. Set it through the environment variable (or its file variant), never commit it. |
| `auth.saml.spSigningCert` | `LAHIJAN_AUTH_SAML_SPSIGNINGCERT` | string | empty | PEM-encoded X.509 certificate that matches `spSigningKey`. It is published in the service provider metadata so identity providers can verify Lahijan's requests. Required outside development. |
| `auth.saml.redirectBase` | `LAHIJAN_AUTH_SAML_REDIRECTBASE` | string | empty | Public base URL that Lahijan advertises for its SAML endpoints, for example `https://app.example.com`. Empty derives it from the request's host, which only works for local development. |
| `auth.saml.jit.enabled` | `LAHIJAN_AUTH_SAML_JIT_ENABLED` | boolean | `false` | Creates a Lahijan account automatically the first time an unknown user signs in through SAML, using the attributes the identity provider sends. Leave it off if an administrator must create accounts first. |
| `auth.saml.providers.entra.enabled` | `LAHIJAN_AUTH_SAML_PROVIDERS_ENTRA_ENABLED` | boolean | `false` | Turns on this SAML identity provider. |
| `auth.saml.providers.entra.entityId` | `LAHIJAN_AUTH_SAML_PROVIDERS_ENTRA_ENTITYID` | string | empty | Entity ID of Lahijan as the service provider, as registered with the identity provider. Usually Lahijan's SAML metadata URL. |
| `auth.saml.providers.entra.idpMetadataXML` | `LAHIJAN_AUTH_SAML_PROVIDERS_ENTRA_IDPMETADATAXML` | string | empty | The identity provider's metadata XML, pasted inline. When set, `idpMetadataURL` is ignored. |
| `auth.saml.providers.entra.idpMetadataURL` | `LAHIJAN_AUTH_SAML_PROVIDERS_ENTRA_IDPMETADATAURL` | string | empty | URL of the identity provider's metadata. Lahijan fetches it once at startup. Used only when `idpMetadataXML` is empty. |
| `auth.saml.providers.entra.allowIdpInitiated` | `LAHIJAN_AUTH_SAML_PROVIDERS_ENTRA_ALLOWIDPINITIATED` | boolean | `false` | Accepts sign-ins that start at the identity provider instead of at Lahijan. Leave it off unless you need it: unsolicited SAML responses are a known cross-site request forgery risk. |
| `auth.saml.providers.entra.emailAttribute` | `LAHIJAN_AUTH_SAML_PROVIDERS_ENTRA_EMAILATTRIBUTE` | string | empty | Name of the SAML attribute that holds the user's email address. Empty uses the standard claim URI that Entra ID, Okta and most providers send. |
| `auth.saml.providers.entra.nameAttribute` | `LAHIJAN_AUTH_SAML_PROVIDERS_ENTRA_NAMEATTRIBUTE` | string | empty | Name of the SAML attribute that holds the user's display name. Empty uses the standard claim URI. |

### `auth.mfa`

Multi-factor authentication. Authenticator app codes (TOTP) and recovery codes work without extra setup; security keys and passkeys (WebAuthn) need `webauthn.rpId` and `webauthn.rpOrigins`.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `auth.mfa.pendingTTLSeconds` | `LAHIJAN_AUTH_MFA_PENDINGTTLSECONDS` | integer | `300` | How long a user has to complete the second factor after entering their password, in seconds. |
| `auth.mfa.maxAttempts` | `LAHIJAN_AUTH_MFA_MAXATTEMPTS` | integer | `5` | Number of wrong second-factor codes allowed in one sign-in attempt. After that the attempt is cancelled and the user must enter their password again. |
| `auth.mfa.totp.issuer` | `LAHIJAN_AUTH_MFA_TOTP_ISSUER` | string | `Lahijan` | Name shown next to the account in the user's authenticator app. |
| `auth.mfa.recovery.count` | `LAHIJAN_AUTH_MFA_RECOVERY_COUNT` | integer | `10` | Number of recovery codes generated in each batch. |
| `auth.mfa.webauthn.rpId` | `LAHIJAN_AUTH_MFA_WEBAUTHN_RPID` | string | empty | WebAuthn relying party ID: the domain users reach the dashboard on, such as `app.example.com`, or a parent domain of it. Empty turns off security keys and passkeys. |
| `auth.mfa.webauthn.rpDisplayName` | `LAHIJAN_AUTH_MFA_WEBAUTHN_RPDISPLAYNAME` | string | `Lahijan` | Name shown in the browser prompt when a user registers or uses a security key. |
| `auth.mfa.webauthn.rpOrigins` | `LAHIJAN_AUTH_MFA_WEBAUTHN_RPORIGINS` | list of strings | empty list | Origins allowed to use WebAuthn, each written as scheme, host and optional port. Must include the dashboard origin, for example `https://app.example.com`. |
| `auth.mfa.webauthn.rpTopOrigins` | `LAHIJAN_AUTH_MFA_WEBAUTHN_RPTOPORIGINS` | list of strings | empty list | Top-level origins allowed when the dashboard is embedded in a frame on another site. Leave it empty unless you embed the dashboard. |

## Email (SMTP)

The SMTP server Lahijan uses to send account emails such as email verification and password reset links.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `smtp.enabled` | `LAHIJAN_SMTP_ENABLED` | boolean | `false` | Sends account emails. When false, no email is sent, so users cannot verify addresses or reset passwords by email. |
| `smtp.host` | `LAHIJAN_SMTP_HOST` | string | `localhost` | Host name of the SMTP server. |
| `smtp.port` | `LAHIJAN_SMTP_PORT` | integer | `1025` | TCP port of the SMTP server, such as 587 for STARTTLS or 1025 for MailHog in development. |
| `smtp.user` | `LAHIJAN_SMTP_USER` | string | empty | SMTP user name. Empty connects without authentication. |
| `smtp.password` | `LAHIJAN_SMTP_PASSWORD` | string | empty | SMTP password. Set it through the environment variable, never commit it. |
| `smtp.from` | `LAHIJAN_SMTP_FROM` | string | `noreply@localhost` | Sender address of every email. |
| `smtp.fromName` | `LAHIJAN_SMTP_FROMNAME` | string | `Lahijan` | Sender name shown next to the address. |
| `smtp.appBaseURL` | `LAHIJAN_SMTP_APPBASEURL` | string | `http://localhost:3000` | Public URL of the dashboard, for example `https://app.example.com`. Links in emails start with it, so a wrong value produces broken links. |

## Background jobs

The durable job queue, built on River and stored in PostgreSQL. Usage metering and other asynchronous work run through it. See [Jobs](/docs/admin/jobs).

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `jobs.enabled` | `LAHIJAN_JOBS_ENABLED` | boolean | `false` | Runs the background job queue. When false, queued work such as usage metering does not run and the jobs admin pages report the feature as disabled. |
| `jobs.maxAttempts` | `LAHIJAN_JOBS_MAXATTEMPTS` | integer | `5` | Number of times a failing job is tried before it is given up, unless the job sets its own limit. |
| `jobs.jobTimeoutSeconds` | `LAHIJAN_JOBS_JOBTIMEOUTSECONDS` | integer | `60` | Maximum run time of one job, in seconds, unless the job sets its own limit. |
| `jobs.softStopTimeoutSeconds` | `LAHIJAN_JOBS_SOFTSTOPTIMEOUTSECONDS` | integer | `30` | On shutdown, how long to wait for running jobs to finish, in seconds, before cancelling them. |
| `jobs.defaultMaxWorkersPerQueue` | `LAHIJAN_JOBS_DEFAULTMAXWORKERSPERQUEUE` | integer | `10` | Number of jobs each queue may run at the same time. Raise it on larger hosts. |
| `jobs.pollOnly` | `LAHIJAN_JOBS_POLLONLY` | boolean | `false` | Checks for new jobs by polling instead of PostgreSQL LISTEN/NOTIFY. Turn it on when Lahijan connects through PgBouncer in transaction pooling mode. |

### `jobs.adminUI`

River's built-in web interface for inspecting and managing jobs.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `jobs.adminUI.enabled` | `LAHIJAN_JOBS_ADMINUI_ENABLED` | boolean | `true` | Serves River's web interface. Only users with the `platform.jobs.read` permission can open it. |
| `jobs.adminUI.path` | `LAHIJAN_JOBS_ADMINUI_PATH` | string | `/admin/jobs/ui` | URL path the jobs web interface is served under. |

## Plugins

The WebAssembly plugin runtime (wazero) and the plugin marketplace. These limits apply to every plugin; a plugin's own manifest can only lower them. See [Plugins](/docs/admin/plugins) and [Marketplace](/docs/admin/marketplace).

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `wasm.enabled` | `LAHIJAN_WASM_ENABLED` | boolean | `false` | Runs the plugin system. When false, plugin administration and the marketplace report the feature as disabled. Plugin records stay in the database, so turning it back on needs no migration. |
| `wasm.maxMemoryPerPlugin` | `LAHIJAN_WASM_MAXMEMORYPERPLUGIN` | integer | `33554432` | Maximum memory one plugin instance may use, in bytes (33554432 is 32 MiB). Plugins that declare a larger memory limit are rejected. |
| `wasm.execTimeoutMs` | `LAHIJAN_WASM_EXECTIMEOUTMS` | integer | `5000` | Maximum time one call into a plugin may take, in milliseconds. Longer calls are cancelled. |
| `wasm.maxModuleSize` | `LAHIJAN_WASM_MAXMODULESIZE` | integer | `10485760` | Maximum size of an uploaded plugin module, in bytes (10485760 is 10 MiB). Keep `http.server.bodylimit` at least this large. |

### `wasm.marketplace`

Where Lahijan reads the plugin marketplace index (`plugins-marketplace.yaml`). When neither `path` nor `url` is set, the marketplace is disabled.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `wasm.marketplace.path` | `LAHIJAN_WASM_MARKETPLACE_PATH` | string | `examples/plugins/marketplace` | Local directory that contains `plugins-marketplace.yaml` at its top level. A relative path is resolved from Lahijan's working directory. Used when `url` is empty. |
| `wasm.marketplace.url` | `LAHIJAN_WASM_MARKETPLACE_URL` | string | empty | HTTP or HTTPS address that serves the marketplace index. When set, it takes precedence over `path`. |
| `wasm.marketplace.cacheTtlSeconds` | `LAHIJAN_WASM_MARKETPLACE_CACHETTLSECONDS` | integer | `60` | How long the marketplace index is cached, in seconds. A local index is also reread when its file changes. |

### `wasm.wasi`

Settings for plugins that run in WASI mode.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `wasm.wasi.fsRoot` | `LAHIJAN_WASM_WASI_FSROOT` | string | `/var/lib/lahijan/wasi-fs` | Directory that holds the files of WASI plugins. Each plugin gets its own subdirectory and cannot reach outside it. The current server does not turn on WASI mode, so this key has no effect yet. |

## Infrastructure backends

Connections to the backends that do the actual work: Incus for compute, PowerDNS for DNS, a domain registrar for domain sales and SeaweedFS for object storage. Each backend is used only when its `enabled` key is true; otherwise the matching feature reports itself as disabled.

### `providers.incus`

How Lahijan connects to the Incus daemon that runs instances, and how tenants map to Incus projects.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `providers.incus.enabled` | `LAHIJAN_PROVIDERS_INCUS_ENABLED` | boolean | `false` | Connects to Incus. When false, compute features report themselves as disabled. When true, Lahijan checks the connection at startup. |
| `providers.incus.socketPath` | `LAHIJAN_PROVIDERS_INCUS_SOCKETPATH` | string | `/var/lib/incus/unix.socket` | Path to the Incus daemon's Unix socket. Used when `remoteURL` is empty. The Lahijan and Incus containers must share this path. |
| `providers.incus.remoteURL` | `LAHIJAN_PROVIDERS_INCUS_REMOTEURL` | string | empty | HTTPS address of a remote Incus daemon, for example `https://incus.lan:8443`. When set, it is used instead of the Unix socket and needs the certificates under `tls`. |
| `providers.incus.requestTimeoutSeconds` | `LAHIJAN_PROVIDERS_INCUS_REQUESTTIMEOUTSECONDS` | integer | `30` | Timeout for one request to Incus, in seconds. Long operations such as copying an image are tracked separately and are not limited by it. |
| `providers.incus.projectPrefix` | `LAHIJAN_PROVIDERS_INCUS_PROJECTPREFIX` | string | `lahijan-tenant-` | Prefix of the Incus project created for each tenant. The project name is this prefix followed by the tenant ID. Do not change it once tenants exist, or Lahijan loses track of their projects. |
| `providers.incus.projectFeatures.images` | `LAHIJAN_PROVIDERS_INCUS_PROJECTFEATURES_IMAGES` | boolean | `true` | Gives each tenant its own images. |
| `providers.incus.projectFeatures.profiles` | `LAHIJAN_PROVIDERS_INCUS_PROJECTFEATURES_PROFILES` | boolean | `true` | Gives each tenant its own profiles. |
| `providers.incus.projectFeatures.networks` | `LAHIJAN_PROVIDERS_INCUS_PROJECTFEATURES_NETWORKS` | boolean | `false` | Gives each tenant its own networks. Incus allows this only with OVN, so leave it off unless your Incus uses OVN. When off, tenant instances attach to `defaultNetwork`. |
| `providers.incus.projectFeatures.storageVolumes` | `LAHIJAN_PROVIDERS_INCUS_PROJECTFEATURES_STORAGEVOLUMES` | boolean | `true` | Gives each tenant its own storage volumes. |
| `providers.incus.projectFeatures.storageBuckets` | `LAHIJAN_PROVIDERS_INCUS_PROJECTFEATURES_STORAGEBUCKETS` | boolean | `true` | Gives each tenant its own storage buckets. |
| `providers.incus.defaultNetwork` | `LAHIJAN_PROVIDERS_INCUS_DEFAULTNETWORK` | string | `lahijanbr` | Incus network added as `eth0` to each tenant's default profile when per-tenant networks are off. `lahijanbr` is the bridge created by the bundled Incus preseed. Empty adds no network interface. |
| `providers.incus.featuredImages` | `LAHIJAN_PROVIDERS_INCUS_FEATUREDIMAGES` | list of strings | `ubuntu/24.04`, `debian/12`, `alpine/3.22`, `fedora/43` | Image aliases shown first in the image catalog, such as `ubuntu/24.04`. Incus must be able to fetch them from its image server; list only releases that the server still publishes. See [Images](/docs/compute/images). |
| `providers.incus.events.enabled` | `LAHIJAN_PROVIDERS_INCUS_EVENTS_ENABLED` | boolean | `true` | Keeps a connection open to the Incus event stream and forwards events to plugins. Turn it off when no real Incus daemon is available. |
| `providers.incus.events.maxReconnectSeconds` | `LAHIJAN_PROVIDERS_INCUS_EVENTS_MAXRECONNECTSECONDS` | integer | `30` | Longest wait between reconnect attempts after the event stream drops, in seconds. |
| `providers.incus.events.maxPayloadBytes` | `LAHIJAN_PROVIDERS_INCUS_EVENTS_MAXPAYLOADBYTES` | integer | `1048576` | Largest event accepted from Incus, in bytes. Larger events are dropped and logged. |
| `providers.incus.tls.serverCert` | `LAHIJAN_PROVIDERS_INCUS_TLS_SERVERCERT` | string | empty | Certificate of the remote Incus server, used to verify it. Needed when the server uses a self-signed certificate. |
| `providers.incus.tls.clientCert` | `LAHIJAN_PROVIDERS_INCUS_TLS_CLIENTCERT` | string | empty | Client certificate that Lahijan presents to Incus. It must be trusted by the Incus daemon. |
| `providers.incus.tls.clientKey` | `LAHIJAN_PROVIDERS_INCUS_TLS_CLIENTKEY` | string | empty | Private key of the client certificate. Set it through the environment variable, never commit it. |
| `providers.incus.tls.insecureSkipVerify` | `LAHIJAN_PROVIDERS_INCUS_TLS_INSECURESKIPVERIFY` | boolean | `false` | Skips verification of the Incus server certificate. For testing only; never turn it on in production. |
| `providers.incus.placement.mode` | `LAHIJAN_PROVIDERS_INCUS_PLACEMENT_MODE` | string | `local` | Where new instances go. `local` lets Incus decide, which is right for a single server. `cluster` makes Lahijan pick the least loaded member of an Incus cluster for each new instance; it requires a clustered Incus. See [Compute cluster](/docs/admin/compute-cluster). |
| `providers.incus.floatingIPs.forwardNetwork` | `LAHIJAN_PROVIDERS_INCUS_FLOATINGIPS_FORWARDNETWORK` | string | empty | Incus network on which Lahijan creates a network forward when a floating IP is attached, so traffic to that IP reaches the instance. Empty means Lahijan does not create forwards and you route floating IPs yourself (for example with BGP or static routes). |

### `providers.powerdns`

How Lahijan connects to the PowerDNS Authoritative server, and defaults for new zones.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `providers.powerdns.enabled` | `LAHIJAN_PROVIDERS_POWERDNS_ENABLED` | boolean | `false` | Connects to PowerDNS. When false, DNS features report themselves as disabled. |
| `providers.powerdns.baseURL` | `LAHIJAN_PROVIDERS_POWERDNS_BASEURL` | string | `http://powerdns:8081` | Address of the PowerDNS HTTP API. |
| `providers.powerdns.apiKey` | `LAHIJAN_PROVIDERS_POWERDNS_APIKEY` | string | empty | API key that PowerDNS expects. Required when PowerDNS is enabled; the server refuses to start without it. Set it through the environment variable, never commit it. |
| `providers.powerdns.requestTimeoutSeconds` | `LAHIJAN_PROVIDERS_POWERDNS_REQUESTTIMEOUTSECONDS` | integer | `30` | Timeout for one request to PowerDNS, in seconds. |
| `providers.powerdns.defaultNameservers` | `LAHIJAN_PROVIDERS_POWERDNS_DEFAULTNAMESERVERS` | list of strings | `ns1.lahijan.local.` | Name servers written into the SOA and NS records of every new zone. The first one is the primary. Use fully qualified names; a trailing dot is added if missing. |
| `providers.powerdns.defaultDNSSECEnabled` | `LAHIJAN_PROVIDERS_POWERDNS_DEFAULTDNSSECENABLED` | boolean | `false` | Turns on DNSSEC for every new zone. When false, DNSSEC is turned on per zone. See [DNSSEC](/docs/dns/dnssec). |
| `providers.powerdns.defaultAXFREnabled` | `LAHIJAN_PROVIDERS_POWERDNS_DEFAULTAXFRENABLED` | boolean | `false` | Intended to allow zone transfers (AXFR) for new zones. The current server does not read this key. |
| `providers.powerdns.defaultAXFRFrom` | `LAHIJAN_PROVIDERS_POWERDNS_DEFAULTAXFRFROM` | list of strings | empty list | IP addresses or CIDR ranges intended to be allowed to transfer new zones. The current server does not read this key. |
| `providers.powerdns.events.enabled` | `LAHIJAN_PROVIDERS_POWERDNS_EVENTS_ENABLED` | boolean | `true` | Sends an event to plugins for every zone and record change Lahijan makes. Only has an effect when the plugin system is on. |

### `providers.registrar`

Domain registration and resale through a registrar reseller account. See [Domains](/docs/dns/domains).

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `providers.registrar.enabled` | `LAHIJAN_PROVIDERS_REGISTRAR_ENABLED` | boolean | `false` | Connects to a domain registrar so users can search, register and renew domains. When false, domain features report themselves as disabled. |
| `providers.registrar.provider` | `LAHIJAN_PROVIDERS_REGISTRAR_PROVIDER` | string | `opensrs` | Registrar to use: `opensrs` or `noop`. `noop` keeps the domain features wired but connects to no registrar, which is useful for testing. |
| `providers.registrar.openSRS.baseURL` | `LAHIJAN_PROVIDERS_REGISTRAR_OPENSRS_BASEURL` | string | `https://rr-n1-tor.opensrs.net` | Address of the OpenSRS reseller API. The default is the production endpoint. |
| `providers.registrar.openSRS.apiKey` | `LAHIJAN_PROVIDERS_REGISTRAR_OPENSRS_APIKEY` | string | empty | OpenSRS reseller API key. Required when the provider is `opensrs`; the server refuses to start without it. Set it through the environment variable, never commit it. |
| `providers.registrar.openSRS.username` | `LAHIJAN_PROVIDERS_REGISTRAR_OPENSRS_USERNAME` | string | empty | OpenSRS reseller account user name. |
| `providers.registrar.openSRS.requestTimeoutSeconds` | `LAHIJAN_PROVIDERS_REGISTRAR_OPENSRS_REQUESTTIMEOUTSECONDS` | integer | `30` | Timeout for one request to OpenSRS, in seconds. |
| `providers.registrar.marginPercent` | `LAHIJAN_PROVIDERS_REGISTRAR_MARGINPERCENT` | integer | `0` | Markup added to the registrar's price, as a whole percentage. `0` sells domains at cost. |
| `providers.registrar.defaultCurrency` | `LAHIJAN_PROVIDERS_REGISTRAR_DEFAULTCURRENCY` | string | `USD` | Currency domain prices are billed in, as a three-letter ISO 4217 code. |

### `providers.seaweedfs`

How Lahijan connects to SeaweedFS. Lahijan manages buckets and credentials; users send object data straight to the SeaweedFS S3 endpoint.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `providers.seaweedfs.enabled` | `LAHIJAN_PROVIDERS_SEAWEEDFS_ENABLED` | boolean | `false` | Connects to SeaweedFS. When false, object storage features report themselves as disabled. |
| `providers.seaweedfs.s3Endpoint` | `LAHIJAN_PROVIDERS_SEAWEEDFS_S3ENDPOINT` | string | `http://seaweedfs:8333` | Address of the SeaweedFS S3 API that Lahijan uses internally. |
| `providers.seaweedfs.publicEndpoint` | `LAHIJAN_PROVIDERS_SEAWEEDFS_PUBLICENDPOINT` | string | empty | Public address of the S3 API that users' S3 clients and browsers reach, for example `https://s3.example.com`. Presigned URLs are signed for this host. Empty uses `s3Endpoint`, which only works inside the Docker network. See [TLS and domains](/docs/operations/tls-and-domains). |
| `providers.seaweedfs.corsAllowedOrigins` | `LAHIJAN_PROVIDERS_SEAWEEDFS_CORSALLOWEDORIGINS` | list of strings | empty list | Browser origins allowed to use presigned URLs, usually the dashboard origin such as `https://app.example.com`. They are written into every bucket's CORS rules. Empty makes uploads and downloads from the dashboard fail. |
| `providers.seaweedfs.usePathStyle` | `LAHIJAN_PROVIDERS_SEAWEEDFS_USEPATHSTYLE` | boolean | `true` | Path-style S3 addresses (`https://host/bucket/key`). SeaweedFS requires them, and the current server always uses them regardless of this key. |
| `providers.seaweedfs.region` | `LAHIJAN_PROVIDERS_SEAWEEDFS_REGION` | string | `us-east-1` | Region name sent with S3 requests. SeaweedFS ignores it, but it must not be empty. |
| `providers.seaweedfs.adminAccessKey` | `LAHIJAN_PROVIDERS_SEAWEEDFS_ADMINACCESSKEY` | string | empty | Access key of the SeaweedFS S3 admin account. It must match the key the SeaweedFS container is started with. Required when SeaweedFS is enabled. Set it through the environment variable, never commit it. |
| `providers.seaweedfs.adminSecretKey` | `LAHIJAN_PROVIDERS_SEAWEEDFS_ADMINSECRETKEY` | string | empty | Secret key of the SeaweedFS S3 admin account. Required when SeaweedFS is enabled. Set it through the environment variable, never commit it. |
| `providers.seaweedfs.filerURL` | `LAHIJAN_PROVIDERS_SEAWEEDFS_FILERURL` | string | `http://seaweedfs:8888` | Address of the SeaweedFS Filer HTTP API, used for bucket metadata and user credentials. |
| `providers.seaweedfs.requestTimeoutSeconds` | `LAHIJAN_PROVIDERS_SEAWEEDFS_REQUESTTIMEOUTSECONDS` | integer | `30` | Timeout for one request to SeaweedFS, in seconds. |
| `providers.seaweedfs.defaultPresignTTLSeconds` | `LAHIJAN_PROVIDERS_SEAWEEDFS_DEFAULTPRESIGNTTLSECONDS` | integer | `3600` | Lifetime of a presigned URL when the request does not set one, in seconds. See [Presigned URLs](/docs/storage/presigned-urls). |
| `providers.seaweedfs.defaultQuotaMiB` | `LAHIJAN_PROVIDERS_SEAWEEDFS_DEFAULTQUOTAMIB` | integer | `0` | Size limit set on each new bucket in SeaweedFS, in MiB. `0` sets no limit in SeaweedFS; Lahijan's own quotas still apply. See [Quotas](/docs/storage/quotas). |
| `providers.seaweedfs.events.enabled` | `LAHIJAN_PROVIDERS_SEAWEEDFS_EVENTS_ENABLED` | boolean | `true` | Sends an event to plugins for every bucket and credential change Lahijan makes. Only has an effect when the plugin system is on. |

## First-run admin

Creates the first platform administrator when Lahijan starts against an empty database. It does nothing once any user exists. See [Installation](/docs/getting-started/installation).

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `bootstrap.enabled` | `LAHIJAN_BOOTSTRAP_ENABLED` | boolean | `true` | Creates the first platform administrator on an empty database. When false, no administrator is created and you must add one another way. |
| `bootstrap.adminEmail` | `LAHIJAN_BOOTSTRAP_ADMINEMAIL` | string | empty | Email address of the first administrator. Empty skips the bootstrap. |
| `bootstrap.adminPassword` | `LAHIJAN_BOOTSTRAP_ADMINPASSWORD` | string | empty | Password of the first administrator. Empty generates a random password and prints it once in the server log (`docker compose logs lahijan`). Change it after the first sign-in. Set it through the environment variable, never commit it. |
| `bootstrap.adminDisplayName` | `LAHIJAN_BOOTSTRAP_ADMINDISPLAYNAME` | string | `Platform Administrator` | Display name of the first administrator. |
| `bootstrap.generatedPasswordLength` | `LAHIJAN_BOOTSTRAP_GENERATEDPASSWORDLENGTH` | integer | `24` | Length of the generated password, in characters, when `adminPassword` is empty. |

## Billing

Card payments through Stripe. Balances, top-ups by an administrator and usage charges work without it. See [Payments](/docs/billing/payments) and [Billing administration](/docs/admin/billing).

### `billing.stripe`

The Stripe payment gateway. When it is off, the payment endpoints report the feature as disabled.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `billing.stripe.enabled` | `LAHIJAN_BILLING_STRIPE_ENABLED` | boolean | `false` | Turns on card payments through Stripe. When false, payment endpoints report the feature as disabled and webhook calls from Stripe are accepted and ignored. |
| `billing.stripe.secretKey` | `LAHIJAN_BILLING_STRIPE_SECRETKEY` | string | empty | Stripe secret key (`sk_test_...` or `sk_live_...`). Required when Stripe is enabled. Set it through the environment variable, never commit it. |
| `billing.stripe.publishableKey` | `LAHIJAN_BILLING_STRIPE_PUBLISHABLEKEY` | string | empty | Stripe publishable key (`pk_test_...` or `pk_live_...`). It is sent to the dashboard, so it is not secret. |
| `billing.stripe.webhookSecret` | `LAHIJAN_BILLING_STRIPE_WEBHOOKSECRET` | string | empty | Signing secret of the Stripe webhook endpoint (`whsec_...`), used to verify that webhook calls come from Stripe. Set it through the environment variable, never commit it. |
| `billing.stripe.apiBaseURL` | `LAHIJAN_BILLING_STRIPE_APIBASEURL` | string | `https://api.stripe.com` | Address of the Stripe API. Change it only for testing. |
| `billing.stripe.apiVersion` | `LAHIJAN_BILLING_STRIPE_APIVERSION` | string | empty | Stripe API version to request. Empty uses the version built into Lahijan. |
| `billing.stripe.requestTimeoutSeconds` | `LAHIJAN_BILLING_STRIPE_REQUESTTIMEOUTSECONDS` | integer | `30` | Timeout for one request to Stripe, in seconds. `0` or a negative value falls back to 30 seconds. |

## Agent

The AI agent chat in the dashboard. See [Agent](/docs/agent/overview) and [Agent policy](/docs/admin/agent-policy).

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `agent.enabled` | `LAHIJAN_AGENT_ENABLED` | boolean | `true` | Turns on the AI agent chat. When false, agent features report themselves as disabled. |
| `agent.defaultConversationTitle` | `LAHIJAN_AGENT_DEFAULTCONVERSATIONTITLE` | string | `New conversation` | Title given to a new conversation when the user does not provide one. |
| `agent.maxMessageBytes` | `LAHIJAN_AGENT_MAXMESSAGEBYTES` | integer | `65536` | Largest message a user can send to the agent, in bytes (65536 is 64 KiB). |

### `agent.billing`

What the agent charges to a tenant's balance.

| Setting | Environment variable | Type | Default | Description |
| --- | --- | --- | --- | --- |
| `agent.billing.centsPer1kTokens` | `LAHIJAN_AGENT_BILLING_CENTSPER1KTOKENS` | integer | `2` | Price charged to the tenant's balance for every 1000 model tokens, in cents, when the agent uses a model key provided by the platform. Turns that use the tenant's own model key are free. |
