package zfs

import (
	"fmt"
	"regexp"
	"strings"
)

// snapshotNamePattern is the conservative subset accepted for ZFS snapshot
// short names. ZFS itself accepts a wider grammar, but vm-orchestrator pins
// the alphabet so customer-supplied refs cannot smuggle metacharacters.
var snapshotNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// refPattern is the validator for service-authorized refs (mount names,
// source refs, lease IDs). Same alphabet as snapshot names.
var refPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// ValidateSnapshotName returns an error if the snapshot short name is empty,
// contains '@' or '/', has surrounding whitespace, or violates the pattern.
func ValidateSnapshotName(name string) error {
	if strings.TrimSpace(name) != name || name == "" {
		return fmt.Errorf("zfs snapshot name is required")
	}
	if strings.ContainsAny(name, "@/") {
		return fmt.Errorf("zfs snapshot name must not contain '@' or '/'")
	}
	if !snapshotNamePattern.MatchString(name) {
		return fmt.Errorf("invalid zfs snapshot name %q", name)
	}
	return nil
}

// IsValidRef reports whether s matches the service-authorized ref alphabet.
// Callers that need to thread the result into a context-rich error message
// should prefer this over ValidateRef.
func IsValidRef(s string) bool { return refPattern.MatchString(s) }

// ValidateRef returns a generic error if s is not a valid ref.
func ValidateRef(s string) error {
	if !refPattern.MatchString(s) {
		return fmt.Errorf("invalid ref %q", s)
	}
	return nil
}

// SanitizeComponent replaces characters outside the ref alphabet with '-' and
// returns "mount" if the result is empty. Used to project arbitrary mount
// names into dataset path components.
func SanitizeComponent(value string) string {
	value = strings.TrimSpace(value)
	var builder strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == ':' {
			builder.WriteRune(r)
		} else {
			builder.WriteByte('-')
		}
	}
	if builder.Len() == 0 {
		return "mount"
	}
	return builder.String()
}
