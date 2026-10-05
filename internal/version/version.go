// Package version holds build information. The Makefile sets these values
// with -ldflags at build time.
package version

var (
	// Version is the release version, for example "v0.1.0".
	Version = "dev"
	// Commit is the short git commit hash of the build.
	Commit = "none"
)

// String returns the version and commit in one value.
func String() string {
	return Version + " (" + Commit + ")"
}
