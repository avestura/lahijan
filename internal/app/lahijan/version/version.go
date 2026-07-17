// Package version exposes the build-time version stamp for Lahijan.
// The value is injected via `go build -ldflags "-X ...version.LahijanVersion=..."`.
package version

// LahijanVersion holds the build-time version string ("dev" by default;
// replaced via -ldflags of `go build`).
var LahijanVersion = "dev"
