package bridge

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// piSDKVersion is the pinned version of the @earendil-works PI SDK that the
// bridge runs against. It MUST equal the version pinned in bridge/package.json
// (the repo-local manifest npm uses for typecheck, tests and bundle builds):
// TestPiSDKVersionMatchesSourceManifest fails when the two drift apart, and
// `make sync-pi-sdk` bumps both from the installed PI CLI in one step.
//
// Bumping this constant is not enough to change a running daemon: the bridge
// package template embeds it, so the template hash changes and EnsureBridge
// reinstalls node_modules and rebuilds the bundle on the next start. That is
// the mechanism that keeps a deployed daemon on the pinned SDK.
const piSDKVersion = "0.86.0"

// piSDKPackages are the PI SDK packages installed together at piSDKVersion.
// They are versioned in lockstep upstream, so running them apart is a
// configuration error rather than a supported combination.
var piSDKPackages = []string{
	"@earendil-works/pi-ai",
	"@earendil-works/pi-coding-agent",
}

// sdkDrift is one pinned PI SDK package whose installed version does not match
// piSDKVersion. Installed is "" when the package is absent or unreadable.
type sdkDrift struct {
	Package   string
	Installed string
}

// sdkVersionDrift returns the pinned packages that EnsureBridge must reinstall
// to bring targetDir's node_modules to piSDKVersion. A package is flagged when
// its installed version differs from the pin, including when it is missing —
// so a half-removed tree is repaired rather than accepted. The order follows
// piSDKPackages so logs and tests are deterministic.
func sdkVersionDrift(targetDir string) []sdkDrift {
	drift := make([]sdkDrift, 0, len(piSDKPackages))
	for _, pkg := range piSDKPackages {
		installed := installedSDKVersion(targetDir, pkg)
		if installed == piSDKVersion {
			continue
		}
		drift = append(drift, sdkDrift{Package: pkg, Installed: installed})
	}
	return drift
}

// installedSDKVersion reports the version of one PI SDK package present in
// targetDir's node_modules. It returns "" when the tree or the manifest is
// missing or unreadable — callers must treat that as "differs from the pin",
// never as a match, so a corrupt tree can never look installed.
func installedSDKVersion(targetDir, pkg string) string {
	manifest := filepath.Join(targetDir, "node_modules", pkg, "package.json")
	data, err := os.ReadFile(manifest)
	if err != nil {
		return ""
	}
	var pkgJSON struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkgJSON); err != nil {
		return ""
	}
	return pkgJSON.Version
}
