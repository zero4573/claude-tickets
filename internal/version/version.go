// Package version is tix's version, stamped at build time:
//
//	go build -ldflags "-X github.com/zero4573/tix/internal/version.Version=1.2.3" ./cmd/tix
//
// (the Makefile uses git describe; the Nix package its version).
package version

// Version is "dev" in an unstamped build.
var Version = "dev"
