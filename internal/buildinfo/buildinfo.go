// Package buildinfo exposes version metadata injected at build time via -ldflags.
package buildinfo

import "fmt"

// These values are overridden at build time with -ldflags -X.
var (
	// Version is the semantic version (e.g. "1.2.3") or "dev".
	Version = "dev"
	// Commit is the short git SHA the binary was built from.
	Commit = "none"
	// Date is the RFC3339 build timestamp.
	Date = "unknown"
)

// String returns a single-line, human-readable version string.
func String() string {
	return fmt.Sprintf("ghostwriter %s (commit %s, built %s)", Version, Commit, Date)
}
