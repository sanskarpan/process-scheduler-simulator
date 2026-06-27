// Package version provides build-time version metadata for the server.
// It is injected via -ldflags "-X main.version=..." at build time.
package version

// Version is set at build time; "dev" is the default for local builds.
var Version = "dev"
