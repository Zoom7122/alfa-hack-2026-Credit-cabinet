package release

import (
	"io/fs"
	"regexp"
	"strings"
)

var versionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var generatedPathPattern = regexp.MustCompile(`^(contracts/facts_[0-9a-f]{16}_generated\.go|internal/modelbindings/(facts|model)_[0-9a-f]{16}_generated\.go)$`)

func ValidVersion(version string) bool {
	if !versionPattern.MatchString(version) || strings.HasSuffix(version, ".") {
		return false
	}
	stem := strings.ToUpper(strings.Split(version, ".")[0])
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return false
	}
	return true
}

// AllowedArtifactPath is a fixed generator allowlist, not an arbitrary output
// path beneath the root. It also rejects Windows aliases on every platform.
func AllowedArtifactPath(name string) bool {
	if !fs.ValidPath(name) || strings.ContainsAny(name, "\\:") {
		return false
	}
	switch name {
	case "internal/modelbindings/catalog_generated.go", "contracts/openapi.json", "contracts/scoring_facts.openapi.json", "docs/02_inputs_and_scoring.md", "models/release.json", "models/changes.md", "models/SHA256SUMS":
		return true
	}
	if generatedPathPattern.MatchString(name) {
		return true
	}
	parts := strings.Split(name, "/")
	if len(parts) == 2 && parts[0] == "models" && strings.HasSuffix(parts[1], ".json") {
		return ValidVersion(strings.TrimSuffix(parts[1], ".json"))
	}
	return len(parts) == 4 && parts[0] == "models" && parts[1] == "releases" && ValidVersion(parts[2]) && parts[3] == "manifest.json"
}
func ArchivePath(version string) string  { return "models/releases/" + version + ".zip" }
func ManifestPath(version string) string { return "models/releases/" + version + "/manifest.json" }
