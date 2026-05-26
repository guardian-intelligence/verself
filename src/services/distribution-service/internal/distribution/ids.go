package distribution

import (
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func clean(value string) string {
	return strings.TrimSpace(value)
}

func digestRef(digest string) string {
	return strings.Replace(clean(digest), ":", "-", 1)
}

func digestFromRef(ref string) string {
	return strings.Replace(clean(ref), "-", ":", 1)
}

func digestHex(digest string) string {
	return strings.TrimPrefix(clean(digest), "sha256:")
}

func pgTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func timeFromPG(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func publicOCIReference(publicRegistryURL, repository, digest string) string {
	registry := publicRegistryHost(publicRegistryURL)
	return registry + "/" + strings.Trim(clean(repository), "/") + "@" + clean(digest)
}

func downloadURL(publicRegistryURL, repository, digest string) string {
	registry := publicRegistryHost(publicRegistryURL)
	return "https://" + registry + "/v2/" + strings.Trim(clean(repository), "/") + "/manifests/" + clean(digest)
}

func publicRegistryHost(publicRegistryURL string) string {
	registry := strings.TrimSuffix(clean(publicRegistryURL), "/")
	return strings.TrimPrefix(registry, "https://")
}

func tufMetadataURL(publicBaseURL, packageName string) string {
	return strings.TrimSuffix(clean(publicBaseURL), "/") + "/tuf/" + clean(packageName)
}
