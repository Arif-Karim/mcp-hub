// Package version exposes the build version, set via -ldflags at release time.
package version

// Version is overridden with -ldflags "-X github.com/Arif-Karim/mcp-hub/internal/version.Version=v1.2.3".
var Version = "dev"
