package version

// Version is the current release version. Update this on each release.
const Version = "0.32.2"

// BuildInfo returns a formatted version string.
func BuildInfo() string {
	return "Aurelia v" + Version
}
