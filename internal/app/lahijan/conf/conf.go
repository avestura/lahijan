// Package conf exposes typed getters for every Lahijan configuration key.
// The underlying Viper instance is set up by SetupConfig in setup.go; these
// helpers are the only sanctioned way for the rest of the codebase to read
// config (never call viper.Get* directly outside this package).
package conf

import (
	"net"
	"sort"
	"strconv"

	"github.com/spf13/viper"
)

// IsDebugMode reports whether Lahijan was started with --debug.
func IsDebugMode() bool {
	return viper.GetBool("debug")
}

// GetEnvironment returns the application environment string (dev, stg, prd).
func GetEnvironment() string {
	return viper.GetString("environment")
}

// GetHTTPServerHost returns the host the HTTP server binds to.
func GetHTTPServerHost() string {
	return viper.GetString("http.server.host")
}

// GetHTTPServerPort returns the port the HTTP server listens on.
func GetHTTPServerPort() int {
	return viper.GetInt("http.server.port")
}

// GetHTTPServerAddress returns "host:port" suitable for net.Listen.
func GetHTTPServerAddress() string {
	return net.JoinHostPort(GetHTTPServerHost(), strconv.Itoa(GetHTTPServerPort()))
}

// GetServerBodyLimit returns the maximum request body size in bytes.
func GetServerBodyLimit() int {
	return viper.GetInt("http.server.bodylimit")
}

// GetHTTPServerConcurrency returns Fiber's max concurrent connections.
func GetHTTPServerConcurrency() int {
	return viper.GetInt("http.server.concurrency")
}

// GetHTTPServerPreforkEnabled reports whether Fiber's Prefork is on.
func GetHTTPServerPreforkEnabled() bool {
	return viper.GetBool("http.server.prefork")
}

// GetHTTPServerCORSEnabled reports whether the CORS middleware is enabled.
func GetHTTPServerCORSEnabled() bool {
	return viper.GetBool("http.server.cors.enabled")
}

// GetHTTPServerCORSAllowedHeaders returns the CORS allow-headers list.
func GetHTTPServerCORSAllowedHeaders() []string {
	return viper.GetStringSlice("http.server.cors.allowHeaders")
}

// GetHTTPServerCORSAllowedMethods returns the CORS allow-methods list.
func GetHTTPServerCORSAllowedMethods() []string {
	return viper.GetStringSlice("http.server.cors.allowMethods")
}

// GetHTTPServerCORSAllowedOrigins returns the CORS allow-origins list.
func GetHTTPServerCORSAllowedOrigins() []string {
	return viper.GetStringSlice("http.server.cors.allowOrigins")
}

// GetHTTPServerCORSMaxAge returns the CORS max-age in seconds.
func GetHTTPServerCORSMaxAge() int {
	return viper.GetInt("http.server.cors.maxAge")
}

// GetHTTPServerLoggerEnabled reports whether Fiber's request logger is on.
func GetHTTPServerLoggerEnabled() bool {
	return viper.GetBool("http.server.logger.enabled")
}

// GetHTTPServerLoggerColorsEnabled reports whether colored log output is on.
func GetHTTPServerLoggerColorsEnabled() bool {
	return viper.GetBool("http.server.logger.colors")
}

// GetHTTPServerHealthcheckEnabled reports whether the healthcheck middleware is on.
func GetHTTPServerHealthcheckEnabled() bool {
	return viper.GetBool("http.server.healthcheck.enabled")
}

// GetHTTPServerHealthcheckLivenessEndpoint returns the liveness probe path.
func GetHTTPServerHealthcheckLivenessEndpoint() string {
	return viper.GetString("http.server.healthcheck.livenessEndpoint")
}

// GetHTTPServerHealthcheckReadinessEndpoint returns the readiness probe path.
func GetHTTPServerHealthcheckReadinessEndpoint() string {
	return viper.GetString("http.server.healthcheck.readinessEndpoint")
}

// GetDatabaseHost returns the database host.
func GetDatabaseHost() string {
	return viper.GetString("database.host")
}

// GetDatabasePort returns the database port.
func GetDatabasePort() int {
	return viper.GetInt("database.port")
}

// GetDatabaseName returns the database name.
func GetDatabaseName() string {
	return viper.GetString("database.name")
}

// GetDatabaseUser returns the database user.
func GetDatabaseUser() string {
	return viper.GetString("database.user")
}

// GetDatabasePassword returns the database password.
func GetDatabasePassword() string {
	return viper.GetString("database.password")
}

// GetDatabaseSSLMode returns the database sslmode.
func GetDatabaseSSLMode() string {
	return viper.GetString("database.sslmode")
}

// GetDatabaseMaxConns returns the maximum number of connections in the pool.
func GetDatabaseMaxConns() int {
	return viper.GetInt("database.maxConns")
}

// GetDatabaseMinConns returns the minimum number of idle connections in the pool.
func GetDatabaseMinConns() int {
	return viper.GetInt("database.minConns")
}

// GetDatabaseMaxConnLifetimeSeconds returns the max connection lifetime in seconds.
func GetDatabaseMaxConnLifetimeSeconds() int {
	return viper.GetInt("database.maxConnLifetimeSeconds")
}

// GetDatabaseMaxConnIdleSeconds returns the max connection idle time in seconds.
func GetDatabaseMaxConnIdleSeconds() int {
	return viper.GetInt("database.maxConnIdleSeconds")
}

// GetDatabaseStatementTimeoutMs returns the per-connection statement timeout in
// milliseconds.
func GetDatabaseStatementTimeoutMs() int {
	return viper.GetInt("database.statementTimeoutMs")
}

// ---------------------------------------------------------------------------
// Auth subsystem (WS-06). Every getter reads a key under auth.* or smtp.*.
// ---------------------------------------------------------------------------

// Argon2Params carries the argon2id parameters read from config.
type Argon2Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// GetAuthPasswordArgon2 returns the argon2id hashing parameters.
func GetAuthPasswordArgon2() Argon2Params {
	return Argon2Params{
		Memory:      uint32(viper.GetInt("auth.password.argon2.memory")),
		Iterations:  uint32(viper.GetInt("auth.password.argon2.iterations")),
		Parallelism: uint8(viper.GetInt("auth.password.argon2.parallelism")),
		SaltLength:  uint32(viper.GetInt("auth.password.argon2.saltLength")),
		KeyLength:   uint32(viper.GetInt("auth.password.argon2.keyLength")),
	}
}

// GetAuthPasswordMinLength returns the minimum password length.
func GetAuthPasswordMinLength() int {
	return viper.GetInt("auth.password.minLength")
}

// GetAuthPasswordBreachCheckEnabled reports whether the HIBP k-anonymity breach
// check is enabled.
func GetAuthPasswordBreachCheckEnabled() bool {
	return viper.GetBool("auth.password.breachCheck.enabled")
}

// GetAuthSessionCookieName returns the session cookie name.
func GetAuthSessionCookieName() string {
	return viper.GetString("auth.session.cookieName")
}

// GetAuthRefreshCookieName returns the refresh-token cookie name.
func GetAuthRefreshCookieName() string {
	return viper.GetString("auth.session.refreshCookieName")
}

// GetAuthSessionCookieDomain returns the cookie domain ("" = request host).
func GetAuthSessionCookieDomain() string {
	return viper.GetString("auth.session.domain")
}

// GetAuthSessionCookieSecure reports whether cookies are marked Secure.
func GetAuthSessionCookieSecure() bool {
	return viper.GetBool("auth.session.secure")
}

// GetAuthSessionCookieSameSite returns the SameSite attribute ("strict|lax|none").
func GetAuthSessionCookieSameSite() string {
	return viper.GetString("auth.session.sameSite")
}

// GetAuthSessionCookiePath returns the cookie path.
func GetAuthSessionCookiePath() string {
	return viper.GetString("auth.session.path")
}

// GetAuthSessionLifetimeSeconds returns the session lifetime in seconds.
func GetAuthSessionLifetimeSeconds() int {
	return viper.GetInt("auth.session.lifetimeSeconds")
}

// GetAuthRefreshLifetimeSeconds returns the refresh-token lifetime in seconds.
func GetAuthRefreshLifetimeSeconds() int {
	return viper.GetInt("auth.session.refreshLifetimeSeconds")
}

// GetAuthPATPrefix returns the human-readable prefix prepended to raw PATs.
func GetAuthPATPrefix() string {
	return viper.GetString("auth.pat.prefix")
}

// GetAuthPATByteLength returns the raw PAT entropy length in bytes.
func GetAuthPATByteLength() int {
	return viper.GetInt("auth.pat.byteLength")
}

// GetAuthEmailVerificationTTLSeconds returns the verify-email link TTL in seconds.
func GetAuthEmailVerificationTTLSeconds() int {
	return viper.GetInt("auth.email.verificationTTLSeconds")
}

// GetAuthEmailPasswordResetTTLSeconds returns the password-reset link TTL in seconds.
func GetAuthEmailPasswordResetTTLSeconds() int {
	return viper.GetInt("auth.email.passwordResetTTLSeconds")
}

// GetAuthEmailEmailChangeTTLSeconds returns the email-change link TTL in seconds.
func GetAuthEmailEmailChangeTTLSeconds() int {
	return viper.GetInt("auth.email.emailChangeTTLSeconds")
}

// GetAuthEmailByteLength returns the raw email-link token entropy length in bytes.
func GetAuthEmailByteLength() int {
	return viper.GetInt("auth.email.byteLength")
}

// GetAuthSigningKey returns the HMAC signing key for session cookies and
// email-link tokens. Empty in dev; must be set in prod.
func GetAuthSigningKey() string {
	return viper.GetString("auth.signing.key")
}

// GetSMTPEnabled reports whether outbound email delivery is enabled.
func GetSMTPEnabled() bool {
	return viper.GetBool("smtp.enabled")
}

// GetSMTPHost returns the SMTP host.
func GetSMTPHost() string {
	return viper.GetString("smtp.host")
}

// GetSMTPPort returns the SMTP port.
func GetSMTPPort() int {
	return viper.GetInt("smtp.port")
}

// GetSMTPUser returns the SMTP username.
func GetSMTPUser() string {
	return viper.GetString("smtp.user")
}

// GetSMTPPassword returns the SMTP password.
func GetSMTPPassword() string {
	return viper.GetString("smtp.password")
}

// GetSMTPFrom returns the From: email address.
func GetSMTPFrom() string {
	return viper.GetString("smtp.from")
}

// GetSMTPFromName returns the From: display name.
func GetSMTPFromName() string {
	return viper.GetString("smtp.fromName")
}

// GetSMTPAppBaseURL returns the public dashboard origin used to build email links.
func GetSMTPAppBaseURL() string {
	return viper.GetString("smtp.appBaseURL")
}

// ---------------------------------------------------------------------------
// External identity providers (WS-07a). Getters under auth.secrets.*,
// auth.oauth.*, and auth.oidc.*.
// ---------------------------------------------------------------------------

// GetAuthSecretsEncryptionKey returns the base64-encoded AES-256-GCM key used
// to encrypt external IdP tokens at rest. Empty in dev; must be set in prod.
func GetAuthSecretsEncryptionKey() string {
	return viper.GetString("auth.secrets.encryptionKey")
}

// OAuthProviderConfig carries the configurable fields of one OAuth2 social
// provider (Google, GitHub, generic).
type OAuthProviderConfig struct {
	Enabled      bool
	ClientID     string
	ClientSecret string
	Scopes       []string
}

// GetAuthOAuthProvider returns the config for the named OAuth provider key
// (e.g. "google", "github"). Returns a zero-value (Enabled=false) config when
// the key is absent so callers can simply check .Enabled.
func GetAuthOAuthProvider(name string) OAuthProviderConfig {
	prefix := "auth.oauth.providers." + name
	return OAuthProviderConfig{
		Enabled:      viper.GetBool(prefix + ".enabled"),
		ClientID:     viper.GetString(prefix + ".clientId"),
		ClientSecret: viper.GetString(prefix + ".clientSecret"),
		Scopes:       viper.GetStringSlice(prefix + ".scopes"),
	}
}

// ListAuthOAuthProviderNames returns every OAuth provider key configured under
// auth.oauth.providers.* (sorted). The stable key is what callers use to look
// up the config and what appears in URL paths.
func ListAuthOAuthProviderNames() []string {
	return sortedProviderKeys("auth.oauth.providers")
}

// GetAuthOAuthRedirectBase returns the OAuth redirect base URL. Empty in dev;
// the callback handler derives it from the request Host header.
func GetAuthOAuthRedirectBase() string {
	return viper.GetString("auth.oauth.redirectBase")
}

// OIDCProviderConfig carries the configurable fields of one OIDC provider
// (Keycloak, Auth0, Okta, ...). The Issuer is used for OIDC discovery.
type OIDCProviderConfig struct {
	Enabled      bool
	Issuer       string
	ClientID     string
	ClientSecret string
	Scopes       []string
}

// GetAuthOIDCProvider returns the config for the named OIDC provider key.
// Returns a zero-value (Enabled=false) config when the key is absent.
func GetAuthOIDCProvider(name string) OIDCProviderConfig {
	prefix := "auth.oidc.providers." + name
	return OIDCProviderConfig{
		Enabled:      viper.GetBool(prefix + ".enabled"),
		Issuer:       viper.GetString(prefix + ".issuer"),
		ClientID:     viper.GetString(prefix + ".clientId"),
		ClientSecret: viper.GetString(prefix + ".clientSecret"),
		Scopes:       viper.GetStringSlice(prefix + ".scopes"),
	}
}

// ListAuthOIDCProviderNames returns every OIDC provider key configured under
// auth.oidc.providers.* (sorted).
func ListAuthOIDCProviderNames() []string {
	return sortedProviderKeys("auth.oidc.providers")
}

// GetAuthOIDCRedirectBase returns the OIDC redirect base URL.
func GetAuthOIDCRedirectBase() string {
	return viper.GetString("auth.oidc.redirectBase")
}

// SAMLProviderConfig carries the configurable fields of one SAML 2.0 provider
// (Microsoft Entra, Okta, OneLogin, Shibboleth, Google Workspace SAML). The
// IdP metadata can be supplied either inline (IDPMetadataXML) or by URL
// (IDPMetadataURL); when both are present, the inline XML wins.
type SAMLProviderConfig struct {
	Enabled           bool
	EntityID          string // this SP's entity ID
	IDPMetadataXML    string
	IDPMetadataURL    string
	AllowIDPInitiated bool
	// AttributeMap maps Lahijan field names to SAML attribute names the IdP
	// uses. Empty values fall back to the standard WS-Federation claim URIs.
	EmailAttribute string
	NameAttribute  string
}

// GetAuthSAMLProvider returns the config for the named SAML provider key.
// Returns a zero-value (Enabled=false) config when the key is absent.
func GetAuthSAMLProvider(name string) SAMLProviderConfig {
	prefix := "auth.saml.providers." + name
	return SAMLProviderConfig{
		Enabled:           viper.GetBool(prefix + ".enabled"),
		EntityID:          viper.GetString(prefix + ".entityId"),
		IDPMetadataXML:    viper.GetString(prefix + ".idpMetadataXML"),
		IDPMetadataURL:    viper.GetString(prefix + ".idpMetadataURL"),
		AllowIDPInitiated: viper.GetBool(prefix + ".allowIdpInitiated"),
		EmailAttribute:    viper.GetString(prefix + ".emailAttribute"),
		NameAttribute:     viper.GetString(prefix + ".nameAttribute"),
	}
}

// ListAuthSAMLProviderNames returns every SAML provider key configured under
// auth.saml.providers.* (sorted).
func ListAuthSAMLProviderNames() []string {
	return sortedProviderKeys("auth.saml.providers")
}

// GetAuthSAMLRedirectBase returns the base URL the SP advertises for its ACS
// + metadata endpoints. Empty in dev; the handler derives from the request
// Host header.
func GetAuthSAMLRedirectBase() string {
	return viper.GetString("auth.saml.redirectBase")
}

// GetAuthSAMLSPSigningKey returns the PEM-encoded RSA private key the SP
// uses to sign AuthnRequests + SP metadata. Empty in dev (bootstrap derives
// a deterministic warning value); MUST be set in any non-dev environment.
// Sourced from env LAHIJAN_AUTH_SAML_SP_SIGNING_KEY (or a file path in
// LAHIJAN_AUTH_SAML_SP_SIGNING_KEY_FILE, read by the bootstrap).
func GetAuthSAMLSPSigningKey() string {
	return viper.GetString("auth.saml.spSigningKey")
}

// GetAuthSAMLSPSigningCert returns the PEM-encoded x509 certificate matching
// the SP signing key. Published in the SP metadata so the IdP can verify our
// signed requests.
func GetAuthSAMLSPSigningCert() string {
	return viper.GetString("auth.saml.spSigningCert")
}

// GetAuthSAMLJITEnabled reports whether just-in-time user creation is on for
// SAML login. Default false: production deployments that require admin
// pre-registration keep this off; homelab / small-team deployments turn it
// on via auth.saml.jit.enabled.
func GetAuthSAMLJITEnabled() bool {
	return viper.GetBool("auth.saml.jit.enabled")
}

// GetAuthMFAPendingTTLSeconds returns the pending MFA session lifetime in
// seconds (WS-07c). Default 300 (5 minutes).
func GetAuthMFAPendingTTLSeconds() int {
	return viper.GetInt("auth.mfa.pendingTTLSeconds")
}

// GetAuthMFAMaxAttempts returns the maximum number of consecutive failed
// MFA challenges before a pending session is revoked (WS-07c). Default 5.
func GetAuthMFAMaxAttempts() int {
	return viper.GetInt("auth.mfa.maxAttempts")
}

// GetAuthMFATOTPIssuer returns the human-readable platform name shown in
// the user's authenticator app alongside the account name. Defaults to
// "Lahijan".
func GetAuthMFATOTPIssuer() string {
	return viper.GetString("auth.mfa.totp.issuer")
}

// GetAuthMFARecoveryCount returns the number of recovery codes per batch
// (WS-07c). Default 10.
func GetAuthMFARecoveryCount() int {
	return viper.GetInt("auth.mfa.recovery.count")
}

// GetAuthMFAWebauthnRPID returns the WebAuthn relying-party id (the
// registrable domain). Empty when WebAuthn is disabled (the api handler
// short-circuits with a "feature disabled" envelope).
func GetAuthMFAWebauthnRPID() string {
	return viper.GetString("auth.mfa.webauthn.rpId")
}

// GetAuthMFAWebauthnRPDisplayName returns the human-friendly name shown
// in the browser prompt. Defaults to "Lahijan".
func GetAuthMFAWebauthnRPDisplayName() string {
	return viper.GetString("auth.mfa.webauthn.rpDisplayName")
}

// GetAuthMFAWebauthnRPOrigins returns the list of permitted WebAuthn
// origins (full scheme + host + optional port).
func GetAuthMFAWebauthnRPOrigins() []string {
	return viper.GetStringSlice("auth.mfa.webauthn.rpOrigins")
}

// GetAuthMFAWebauthnRPTopOrigins returns the optional list of permitted
// top origins for Level 3 cross-iframe flows. Empty when not configured.
func GetAuthMFAWebauthnRPTopOrigins() []string {
	return viper.GetStringSlice("auth.mfa.webauthn.rpTopOrigins")
}

// sortedProviderKeys returns the immediate child keys of the providers map at
// the given viper path. Viper exposes nested maps via GetStringMap; the keys
// are returned sorted so callers iterate deterministically (useful for tests
// and for emitting provider lists in error messages).
func sortedProviderKeys(path string) []string {
	m := viper.GetStringMap(path)
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ---------------------------------------------------------------------------
// Jobs subsystem (WS-09). Every getter reads a key under jobs.*.
// ---------------------------------------------------------------------------

// GetJobsEnabled reports whether the durable-job subsystem (River) is wired
// into this process. When false, program.Start skips building the client +
// supervisor and the admin jobs API degrades to 501. Useful for tests and
// for very small deployments that do not need a queue.
func GetJobsEnabled() bool {
	return viper.GetBool("jobs.enabled")
}

// GetJobsAdminUIEnabled reports whether the River web UI is mounted at
// /admin/jobs/ui. The UI is admin-only via RequirePerm; this flag lets
// operators disable it without disabling the queue itself.
func GetJobsAdminUIEnabled() bool {
	return viper.GetBool("jobs.adminUI.enabled")
}

// GetJobsAdminUIPath returns the path the River web UI is mounted under.
// Defaults to /admin/jobs/ui.
func GetJobsAdminUIPath() string {
	return viper.GetString("jobs.adminUI.path")
}

// GetJobsSoftStopTimeoutSeconds returns the max wait for in-flight jobs to
// finish during a graceful shutdown before escalating to a hard cancel.
func GetJobsSoftStopTimeoutSeconds() int {
	return viper.GetInt("jobs.softStopTimeoutSeconds")
}

// GetJobsMaxAttempts returns the default retry budget for jobs that do not
// pin their own MaxAttempts at insert time.
func GetJobsMaxAttempts() int {
	return viper.GetInt("jobs.maxAttempts")
}

// GetJobsJobTimeoutSeconds returns the default per-job wall-clock timeout
// applied at the client level. A job can override this via its worker's
// Timeout() method.
func GetJobsJobTimeoutSeconds() int {
	return viper.GetInt("jobs.jobTimeoutSeconds")
}

// GetJobsDefaultMaxWorkersPerQueue returns the per-queue MaxWorkers when the
// queue is not explicitly listed in jobs.queues.*.
func GetJobsDefaultMaxWorkersPerQueue() int {
	return viper.GetInt("jobs.defaultMaxWorkersPerQueue")
}

// GetJobsPollOnly reports whether the River client should run in poll-only
// mode (no LISTEN/NOTIFY). Useful behind PgBouncer transaction pooling.
func GetJobsPollOnly() bool {
	return viper.GetBool("jobs.pollOnly")
}

// ---------------------------------------------------------------------------
// WASM plugin subsystem (WS-10a). Every getter reads a key under wasm.*.
// ---------------------------------------------------------------------------

// GetWasmEnabled reports whether the WASM plugin subsystem is wired into
// this process. When false, program.Start skips building the wazero runtime
// and the admin plugin API degrades to 501. Useful for tests and for
// deployments that do not want the plugin surface at all.
func GetWasmEnabled() bool {
	return viper.GetBool("wasm.enabled")
}

// GetWasmMaxMemoryPerPlugin returns the per-instance memory cap in bytes.
// Modules whose declared memory max exceeds this are rejected at compile
// time so a single plugin cannot reserve gigabytes of address space.
func GetWasmMaxMemoryPerPlugin() int {
	return viper.GetInt("wasm.maxMemoryPerPlugin")
}

// GetWasmExecTimeoutMs returns the per-call wall-clock timeout in
// milliseconds. A plugin call that exceeds this is cancelled via context.
func GetWasmExecTimeoutMs() int {
	return viper.GetInt("wasm.execTimeoutMs")
}

// GetWasmMaxModuleSize returns the max accepted .wasm upload size in bytes.
// The admin upload handler rejects anything larger at the multipart boundary.
func GetWasmMaxModuleSize() int {
	return viper.GetInt("wasm.maxModuleSize")
}

// ---------------------------------------------------------------------------
// WASM plugin marketplace (WS-10c). Every getter reads a key under
// wasm.marketplace.*.
// ---------------------------------------------------------------------------

// GetWasmMarketplacePath returns the local marketplace directory. Used
// when wasm.marketplace.url is empty. Defaults to the in-repo
// examples/plugins/marketplace/ directory.
func GetWasmMarketplacePath() string {
	return viper.GetString("wasm.marketplace.path")
}

// GetWasmMarketplaceURL returns the remote HTTP(S) endpoint serving the
// marketplace index. When non-empty, takes precedence over the local
// path. Empty string means "use the local path".
func GetWasmMarketplaceURL() string {
	return viper.GetString("wasm.marketplace.url")
}

// GetWasmMarketplaceCacheTTL returns the parsed-index cache lifetime.
// The local loader also watches the file's mtime; the HTTP loader only
// re-fetches on TTL expiry.
func GetWasmMarketplaceCacheTTL() int {
	return viper.GetInt("wasm.marketplace.cacheTtlSeconds")
}

// ---------------------------------------------------------------------------
// Infrastructure providers (WS-11..13). Getters under providers.<name>.*.
// ---------------------------------------------------------------------------

// GetProvidersIncusEnabled reports whether the Incus compute driver (WS-11)
// is wired into this process. When false, program.Start skips building the
// driver and the compute module degrades to 501 "feature disabled".
func GetProvidersIncusEnabled() bool {
	return viper.GetBool("providers.incus.enabled")
}

// GetProvidersIncusSocketPath returns the Incus daemon Unix socket path.
// Empty means "use the HTTPS remote URL instead".
func GetProvidersIncusSocketPath() string {
	return viper.GetString("providers.incus.socketPath")
}

// GetProvidersIncusRemoteURL returns the optional HTTPS remote Incus URL.
// Empty in dev (Unix socket wins).
func GetProvidersIncusRemoteURL() string {
	return viper.GetString("providers.incus.remoteURL")
}

// GetProvidersIncusRequestTimeoutSeconds returns the per-REST-call timeout.
func GetProvidersIncusRequestTimeoutSeconds() int {
	return viper.GetInt("providers.incus.requestTimeoutSeconds")
}

// GetProvidersIncusProjectPrefix returns the prefix prepended to the tenant
// UUID to form the Incus project name. Default "lahijan-tenant-".
func GetProvidersIncusProjectPrefix() string {
	return viper.GetString("providers.incus.projectPrefix")
}

// IncusProjectFeatures carries the project-feature flags applied at tenant
// bootstrap. Each isolates a resource class inside the per-tenant Incus
// project.
type IncusProjectFeatures struct {
	Images         bool
	Profiles       bool
	Networks       bool
	StorageVolumes bool
	StorageBuckets bool
}

// GetProvidersIncusProjectFeatures returns the project-feature flags.
func GetProvidersIncusProjectFeatures() IncusProjectFeatures {
	return IncusProjectFeatures{
		Images:         viper.GetBool("providers.incus.projectFeatures.images"),
		Profiles:       viper.GetBool("providers.incus.projectFeatures.profiles"),
		Networks:       viper.GetBool("providers.incus.projectFeatures.networks"),
		StorageVolumes: viper.GetBool("providers.incus.projectFeatures.storageVolumes"),
		StorageBuckets: viper.GetBool("providers.incus.projectFeatures.storageBuckets"),
	}
}

// GetProvidersIncusFeaturedImages returns the list of image aliases
// advertised as "featured" by the compute module's image catalog.
func GetProvidersIncusFeaturedImages() []string {
	return viper.GetStringSlice("providers.incus.featuredImages")
}

// GetProvidersIncusEventsEnabled reports whether the events listener should
// start at bootstrap. Disable for dev runs that do not have a real daemon.
func GetProvidersIncusEventsEnabled() bool {
	return viper.GetBool("providers.incus.events.enabled")
}

// GetProvidersIncusEventsMaxReconnectSeconds returns the upper bound on the
// reconnect backoff applied by the events listener.
func GetProvidersIncusEventsMaxReconnectSeconds() int {
	return viper.GetInt("providers.incus.events.maxReconnectSeconds")
}

// GetProvidersIncusEventsMaxPayloadBytes returns the cap on a single event
// payload received from the Incus daemon. Larger events are dropped.
func GetProvidersIncusEventsMaxPayloadBytes() int {
	return viper.GetInt("providers.incus.events.maxPayloadBytes")
}

// IncusTLSConfig carries the mTLS material for HTTPS remote Incus.
type IncusTLSConfig struct {
	ServerCert         string
	ClientCert         string
	ClientKey          string
	InsecureSkipVerify bool
}

// GetProvidersIncusTLS returns the mTLS material for HTTPS remote Incus.
// All four fields are empty in Unix-socket mode.
func GetProvidersIncusTLS() IncusTLSConfig {
	return IncusTLSConfig{
		ServerCert:         viper.GetString("providers.incus.tls.serverCert"),
		ClientCert:         viper.GetString("providers.incus.tls.clientCert"),
		ClientKey:          viper.GetString("providers.incus.tls.clientKey"),
		InsecureSkipVerify: viper.GetBool("providers.incus.tls.insecureSkipVerify"),
	}
}

// ---------------------------------------------------------------------------
// PowerDNS provider (WS-12). Every getter reads a key under providers.powerdns.*.
// ---------------------------------------------------------------------------

// GetProvidersPowerDNSEnabled reports whether the PowerDNS DNS driver (WS-12)
// is wired into this process. When false, program.Start skips building the
// driver and the DNS module (WS-15) degrades to 501 "feature disabled".
func GetProvidersPowerDNSEnabled() bool {
	return viper.GetBool("providers.powerdns.enabled")
}

// GetProvidersPowerDNSBaseURL returns the PDNS HTTP API base URL. The
// default targets the compose service on port 8081.
func GetProvidersPowerDNSBaseURL() string {
	return viper.GetString("providers.powerdns.baseURL")
}

// GetProvidersPowerDNSAPIKey returns the secret sent on every PDNS request
// in the X-API-Key header. SENSITIVE — never logged. Must be set via env
// LAHIJAN_PROVIDERS_POWERDNS_API_KEY in any non-dev environment.
func GetProvidersPowerDNSAPIKey() string {
	return viper.GetString("providers.powerdns.apiKey")
}

// GetProvidersPowerDNSRequestTimeoutSeconds returns the per-call timeout
// applied to every PDNS HTTP request.
func GetProvidersPowerDNSRequestTimeoutSeconds() int {
	return viper.GetInt("providers.powerdns.requestTimeoutSeconds")
}

// GetProvidersPowerDNSDefaultNameservers returns the NS targets the driver
// writes into the SOA + NS RRsets at zone-create time when the caller does
// not override them. Each entry must be a canonical name.
func GetProvidersPowerDNSDefaultNameservers() []string {
	return viper.GetStringSlice("providers.powerdns.defaultNameservers")
}

// GetProvidersPowerDNSDefaultDNSSECEnabled reports whether new zones get
// DNSSEC turned on at create time. Default false per WS-12 "Open questions"
// item 1.
func GetProvidersPowerDNSDefaultDNSSECEnabled() bool {
	return viper.GetBool("providers.powerdns.defaultDNSSECEnabled")
}

// GetProvidersPowerDNSDefaultAXFREnabled reports whether new zones get
// AXFR allowed at create time. Default false per WS-12 "Open questions"
// item 2 (Lahijan is the only NS by default).
func GetProvidersPowerDNSDefaultAXFREnabled() bool {
	return viper.GetBool("providers.powerdns.defaultAXFREnabled")
}

// GetProvidersPowerDNSDefaultAXFRFrom returns the IPs/CIDRs written into
// ALLOW-AXFR-FROM at zone-create time when defaultAXFREnabled is true.
// Empty when AXFR is off.
func GetProvidersPowerDNSDefaultAXFRFrom() []string {
	return viper.GetStringSlice("providers.powerdns.defaultAXFRFrom")
}

// GetProvidersPowerDNSEventsEnabled reports whether the driver should
// synthesize change events into the WASM event bus. PDNS does not push
// events itself; the driver emits one BusEvent per mutating call when this
// is true and a bus is wired.
func GetProvidersPowerDNSEventsEnabled() bool {
	return viper.GetBool("providers.powerdns.events.enabled")
}

// ---------------------------------------------------------------------------
// SeaweedFS provider (WS-13). Every getter reads a key under providers.seaweedfs.*.
// ---------------------------------------------------------------------------

// GetProvidersSeaweedFSEnabled reports whether the SeaweedFS object storage
// driver (WS-13) is wired into this process. When false, program.Start
// skips building the driver and the storage module (WS-16) degrades to
// 501 "feature disabled".
func GetProvidersSeaweedFSEnabled() bool {
	return viper.GetBool("providers.seaweedfs.enabled")
}

// GetProvidersSeaweedFSS3Endpoint returns the S3 endpoint URL the AWS SDK
// client targets. Default targets the compose service on port 8333.
func GetProvidersSeaweedFSS3Endpoint() string {
	return viper.GetString("providers.seaweedfs.s3Endpoint")
}

// GetProvidersSeaweedFSUsePathStyle reports whether the S3 client should
// use path-style addressing. SeaweedFS requires path-style; setting this
// to false breaks the driver.
func GetProvidersSeaweedFSUsePathStyle() bool {
	return viper.GetBool("providers.seaweedfs.usePathStyle")
}

// GetProvidersSeaweedFSRegion returns the AWS region the SDK presents on
// every request. SeaweedFS ignores the region for auth purposes but the
// SDK requires a non-empty value.
func GetProvidersSeaweedFSRegion() string {
	return viper.GetString("providers.seaweedfs.region")
}

// GetProvidersSeaweedFSAdminAccessKey returns the admin access key the
// SeaweedFS S3 server recognises as the root account. SENSITIVE — never
// logged. Sourced from env LAHIJAN_PROVIDERS_SEAWEEDFS_ADMIN_ACCESS_KEY.
func GetProvidersSeaweedFSAdminAccessKey() string {
	return viper.GetString("providers.seaweedfs.adminAccessKey")
}

// GetProvidersSeaweedFSAdminSecretKey returns the admin secret key paired
// with the admin access key. SENSITIVE — never logged.
func GetProvidersSeaweedFSAdminSecretKey() string {
	return viper.GetString("providers.seaweedfs.adminSecretKey")
}

// GetProvidersSeaweedFSFilerURL returns the Filer HTTP API origin the
// driver uses for Filer metadata + IAM writes. Default targets the
// compose service on port 8888.
func GetProvidersSeaweedFSFilerURL() string {
	return viper.GetString("providers.seaweedfs.filerURL")
}

// GetProvidersSeaweedFSRequestTimeoutSeconds returns the per-call timeout
// applied to every S3 + Filer HTTP request.
func GetProvidersSeaweedFSRequestTimeoutSeconds() int {
	return viper.GetInt("providers.seaweedfs.requestTimeoutSeconds")
}

// GetProvidersSeaweedFSDefaultPresignTTLSeconds returns the default
// pre-signed URL lifetime in seconds when the caller does not override
// it. 3600 (1 hour) is the AWS-recommended ceiling.
func GetProvidersSeaweedFSDefaultPresignTTLSeconds() int {
	return viper.GetInt("providers.seaweedfs.defaultPresignTTLSeconds")
}

// GetProvidersSeaweedFSDefaultQuotaMiB returns the default per-bucket
// quota in mebibytes applied at bucket-create time. 0 means no backend
// quota (Lahijan enforces via metering instead).
func GetProvidersSeaweedFSDefaultQuotaMiB() int64 {
	return viper.GetInt64("providers.seaweedfs.defaultQuotaMiB")
}

// GetProvidersSeaweedFSEventsEnabled reports whether the driver should
// synthesize change events into the WASM event bus. SeaweedFS does not
// push events itself; the driver emits one BusEvent per mutating call
// when this is true and a bus is wired.
func GetProvidersSeaweedFSEventsEnabled() bool {
	return viper.GetBool("providers.seaweedfs.events.enabled")
}

// ---------------------------------------------------------------------------
// First-run admin bootstrap (WS-23). Every getter reads a key under
// bootstrap.*. The bootstrap runs at program.Start time after RBAC seeding
// when the DB has zero users; it creates a platform.admin user from the
// configured email + password (generated if absent).
// ---------------------------------------------------------------------------

// GetBootstrapEnabled reports whether the first-run admin bootstrap is on.
// When false, program.Start never attempts to create the platform.admin
// user even on an empty database.
func GetBootstrapEnabled() bool {
	return viper.GetBool("bootstrap.enabled")
}

// GetBootstrapAdminEmail returns the email of the platform.admin user to
// create on first run. Empty string means "skip bootstrap" (even when
// enabled is true). Sourced from env LAHIJAN_BOOTSTRAP_ADMIN_EMAIL.
func GetBootstrapAdminEmail() string {
	return viper.GetString("bootstrap.adminEmail")
}

// GetBootstrapAdminPassword returns the optional pre-set password for the
// bootstrap admin. Empty means "generate a random one and print it once".
// Sourced from env LAHIJAN_BOOTSTRAP_ADMIN_PASSWORD.
func GetBootstrapAdminPassword() string {
	return viper.GetString("bootstrap.adminPassword")
}

// GetBootstrapAdminDisplayName returns the display name written on the
// bootstrap admin row. Defaults to "Platform Administrator".
func GetBootstrapAdminDisplayName() string {
	return viper.GetString("bootstrap.adminDisplayName")
}

// GetBootstrapGeneratedPasswordLength returns the length of the random
// password the bootstrap mints when adminPassword is empty. Default 24.
func GetBootstrapGeneratedPasswordLength() int {
	return viper.GetInt("bootstrap.generatedPasswordLength")
}
