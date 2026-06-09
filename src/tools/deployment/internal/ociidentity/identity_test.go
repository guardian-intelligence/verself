package ociidentity

import "testing"

func TestNewPacket(t *testing.T) {
	t.Parallel()
	rootDigest := testDigest("root")
	manifestDigest := testDigest("manifest")
	packet, err := NewPacket(
		WorkloadCoordinates{
			Site:           "prod",
			NomadNamespace: "default",
			NomadJob:       "analytics-service",
			NomadGroup:     "analytics-service",
			NomadTask:      "analytics-service",
		},
		OCIDigests{
			ArchiveSHA256:   testDigest("archive"),
			RootDigests:     []string{rootDigest},
			ManifestDigests: []string{manifestDigest},
		},
	)
	if err != nil {
		t.Fatalf("NewPacket() error = %v", err)
	}
	wantSelectors := []string{
		"site:prod",
		"nomad:namespace:default",
		"nomad:job:analytics-service",
		"nomad:group:analytics-service",
		"nomad:task:analytics-service",
		"oci:root-digest:" + rootDigest,
		"oci:manifest-digest:" + manifestDigest,
	}
	if !sameStrings(packet.Selectors, wantSelectors) {
		t.Fatalf("Selectors = %#v, want %#v", packet.Selectors, wantSelectors)
	}
}

func TestNewPacketRejectsAmbiguousCoordinate(t *testing.T) {
	t.Parallel()
	_, err := NewPacket(
		WorkloadCoordinates{
			Site:           "prod",
			NomadNamespace: "default",
			NomadJob:       "analytics:service",
			NomadGroup:     "analytics-service",
			NomadTask:      "analytics-service",
		},
		OCIDigests{
			ArchiveSHA256:   testDigest("archive"),
			RootDigests:     []string{testDigest("root")},
			ManifestDigests: []string{testDigest("manifest")},
		},
	)
	if err == nil {
		t.Fatal("NewPacket() error = nil, want selector delimiter error")
	}
}
