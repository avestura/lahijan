package conf

import (
	"net"
	"strconv"

	"github.com/spf13/viper"
)

func IsDebugMode() bool {
	return viper.GetBool("debug")
}

func GetHttpServerHost() string {
	return viper.GetString("http.server.host")
}

func GetHttpServerPort() int {
	return viper.GetInt("http.server.port")
}

func GetHttpServerAddress() string {
	return net.JoinHostPort(GetHttpServerHost(), strconv.Itoa(GetHttpServerPort()))
}

func GetServerBodyLimit() int {
	return viper.GetInt("http.server.bodylimit")
}

func GetHttpServerConcurrency() int {
	return viper.GetInt("http.server.concurrency")
}

func GetHttpServerPreforkEnabled() bool {
	return viper.GetBool("http.server.prefork")
}

func GetHttpServerCorsEnabled() bool {
	return viper.GetBool("http.server.cors.enabled")
}

func GetHttpServerCorsAllowedHeaders() []string {
	return viper.GetStringSlice("http.server.cors.allowHeaders")
}

func GetHttpServerCorsAllowedMethods() []string {
	return viper.GetStringSlice("http.server.cors.allowMethods")
}

func GetHttpServerCorsAllowedOrigins() []string {
	return viper.GetStringSlice("http.server.cors.allowOrigins")
}

func GetHttpServerCorsMaxAge() int {
	return viper.GetInt("http.server.cors.maxAge")
}

func GetHttpServerLoggerEnabled() bool {
	return viper.GetBool("http.server.logger.enabled")
}

func GetHttpServerLoggerColorsEnabled() bool {
	return viper.GetBool("http.server.logger.colors")
}

func GetHttpServerHealthcheckEnabled() bool {
	return viper.GetBool("http.server.healthcheck.enabled")
}

func GetHttpServerHealthcheckLivenessEndpoint() string {
	return viper.GetString("http.server.healthcheck.livenessEndpoint")
}

func GetHttpServerHealthcheckReadinessEndpoint() string {
	return viper.GetString("http.server.healthcheck.readinessEndpoint")
}
