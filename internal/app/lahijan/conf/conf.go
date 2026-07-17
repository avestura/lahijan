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
