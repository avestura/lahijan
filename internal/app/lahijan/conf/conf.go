// Package conf exposes typed getters for every Lahijan configuration key.
// The underlying Viper instance is set up by SetupConfig in setup.go; these
// helpers are the only sanctioned way for the rest of the codebase to read
// config (never call viper.Get* directly outside this package).
package conf

import (
	"net"
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
