// Package version is ct's version, stamped at build time:
//
//	go build -ldflags "-X github.com/zero4573/claude-tickets/internal/version.Version=1.2.3" ./cmd/ct
//
// (the Makefile uses git describe; the Nix package its version).
package version

// Version is "dev" in an unstamped build.
var Version = "dev"
