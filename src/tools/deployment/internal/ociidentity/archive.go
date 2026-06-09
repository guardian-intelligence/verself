package ociidentity

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

const maxOCIJSONBlobBytes int64 = 8 << 20

// OCIDigests captures stable digests from an OCI image-layout archive.
type OCIDigests struct {
	ArchiveSHA256   string   `json:"oci_archive_sha256"`
	RootDigests     []string `json:"oci_root_digests"`
	ManifestDigests []string `json:"oci_manifest_digests"`
}

type ociDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

type ociIndex struct {
	SchemaVersion int             `json:"schemaVersion"`
	MediaType     string          `json:"mediaType"`
	Manifests     []ociDescriptor `json:"manifests"`
}

// ReadArchiveDigests reads an OCI image-layout tar archive and returns the
// registry-addressable digest set needed to identify a workload image.
func ReadArchiveDigests(archivePath string) (digests OCIDigests, err error) {
	if archivePath == "" {
		return OCIDigests{}, errors.New("ociidentity: OCI archive path is required")
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return OCIDigests{}, fmt.Errorf("ociidentity: open OCI archive: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("ociidentity: close OCI archive: %w", closeErr)
		}
	}()

	archiveHash := sha256.New()
	reader := tar.NewReader(io.TeeReader(file, archiveHash))
	indexJSON, blobs, err := readOCIArchive(reader)
	if err != nil {
		return OCIDigests{}, err
	}
	if len(indexJSON) == 0 {
		return OCIDigests{}, errors.New("ociidentity: OCI archive is missing index.json")
	}

	var index ociIndex
	if err := json.Unmarshal(indexJSON, &index); err != nil {
		return OCIDigests{}, fmt.Errorf("ociidentity: parse index.json: %w", err)
	}
	if len(index.Manifests) == 0 {
		return OCIDigests{}, errors.New("ociidentity: index.json has no manifests")
	}

	rootDigests := make([]string, 0, len(index.Manifests))
	manifestSet := map[string]struct{}{}
	for _, descriptor := range index.Manifests {
		if err := validateDigest(descriptor.Digest); err != nil {
			return OCIDigests{}, fmt.Errorf("ociidentity: root descriptor: %w", err)
		}
		rootDigests = append(rootDigests, descriptor.Digest)
		if err := walkDescriptor(descriptor, blobs, manifestSet); err != nil {
			return OCIDigests{}, err
		}
	}

	return OCIDigests{
		ArchiveSHA256:   "sha256:" + hex.EncodeToString(archiveHash.Sum(nil)),
		RootDigests:     uniqueSorted(rootDigests),
		ManifestDigests: sortedSet(manifestSet),
	}, nil
}

func readOCIArchive(reader *tar.Reader) ([]byte, map[string][]byte, error) {
	var indexJSON []byte
	blobs := map[string][]byte{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return indexJSON, blobs, nil
		}
		if err != nil {
			return nil, nil, fmt.Errorf("ociidentity: read OCI archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}

		name := path.Clean(header.Name)
		switch {
		case name == "index.json":
			body, err := readBounded(reader, header.Size, maxOCIJSONBlobBytes, "index.json")
			if err != nil {
				return nil, nil, err
			}
			indexJSON = body
		case strings.HasPrefix(name, "blobs/sha256/"):
			digestHex := strings.TrimPrefix(name, "blobs/sha256/")
			if len(digestHex) != sha256.Size*2 || header.Size > maxOCIJSONBlobBytes {
				if _, err := io.Copy(io.Discard, reader); err != nil {
					return nil, nil, fmt.Errorf("ociidentity: discard blob %s: %w", name, err)
				}
				continue
			}
			body, err := readBounded(reader, header.Size, maxOCIJSONBlobBytes, name)
			if err != nil {
				return nil, nil, err
			}
			if err := verifyBlobDigest(digestHex, body); err != nil {
				return nil, nil, err
			}
			blobs["sha256:"+digestHex] = body
		default:
			if _, err := io.Copy(io.Discard, reader); err != nil {
				return nil, nil, fmt.Errorf("ociidentity: discard archive member %s: %w", name, err)
			}
		}
	}
}

func readBounded(reader io.Reader, size int64, maxBytes int64, label string) ([]byte, error) {
	if size < 0 || size > maxBytes {
		return nil, fmt.Errorf("ociidentity: %s is %d bytes, max %d", label, size, maxBytes)
	}
	body, err := io.ReadAll(io.LimitReader(reader, size))
	if err != nil {
		return nil, fmt.Errorf("ociidentity: read %s: %w", label, err)
	}
	if int64(len(body)) != size {
		return nil, fmt.Errorf("ociidentity: read %s: got %d bytes, want %d", label, len(body), size)
	}
	return body, nil
}

func verifyBlobDigest(digestHex string, body []byte) error {
	actual := sha256.Sum256(body)
	if hex.EncodeToString(actual[:]) != digestHex {
		return fmt.Errorf("ociidentity: blob sha256:%s digest mismatch", digestHex)
	}
	return nil
}

func walkDescriptor(descriptor ociDescriptor, blobs map[string][]byte, manifestSet map[string]struct{}) error {
	if err := validateDigest(descriptor.Digest); err != nil {
		return err
	}
	if isManifestMediaType(descriptor.MediaType) {
		if _, ok := blobs[descriptor.Digest]; !ok {
			return fmt.Errorf("ociidentity: manifest blob %s is missing from archive", descriptor.Digest)
		}
		manifestSet[descriptor.Digest] = struct{}{}
		return nil
	}
	if !isIndexMediaType(descriptor.MediaType) {
		return fmt.Errorf("ociidentity: descriptor %s has unsupported media type %q", descriptor.Digest, descriptor.MediaType)
	}

	body, ok := blobs[descriptor.Digest]
	if !ok {
		return fmt.Errorf("ociidentity: index blob %s is missing from archive", descriptor.Digest)
	}
	var index ociIndex
	if err := json.Unmarshal(body, &index); err != nil {
		return fmt.Errorf("ociidentity: parse index blob %s: %w", descriptor.Digest, err)
	}
	if len(index.Manifests) == 0 {
		return fmt.Errorf("ociidentity: index blob %s has no manifests", descriptor.Digest)
	}
	for _, child := range index.Manifests {
		if err := walkDescriptor(child, blobs, manifestSet); err != nil {
			return err
		}
	}
	return nil
}

func isIndexMediaType(mediaType string) bool {
	return mediaType == "application/vnd.oci.image.index.v1+json" ||
		mediaType == "application/vnd.docker.distribution.manifest.list.v2+json"
}

func isManifestMediaType(mediaType string) bool {
	return mediaType == "application/vnd.oci.image.manifest.v1+json" ||
		mediaType == "application/vnd.docker.distribution.manifest.v2+json"
}

func sortedSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func uniqueSorted(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return sortedSet(set)
}
