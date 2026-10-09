// Package version carries build metadata.
package version

// These are overridable at build time with -ldflags "-X ...".
var (
	Version = "v1.0.0"
	Build   = "dev"
	Commit  = "unknown"
)
