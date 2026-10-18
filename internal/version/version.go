// Package version carries build metadata.
package version

// These are overridable at build time with -ldflags "-X ...".
var (
	Version = "dev"
	Build   = "dev"
	Commit  = "unknown"
)
