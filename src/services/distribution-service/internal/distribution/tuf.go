package distribution

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type TUFSigner struct {
	KeyID      string
	PrivateKey ed25519.PrivateKey
	PublicKey  ed25519.PublicKey
}

func NewTUFSigner(keyID string, privateKeyBase64 string) (TUFSigner, error) {
	decoded, err := base64.StdEncoding.DecodeString(clean(privateKeyBase64))
	if err != nil {
		return TUFSigner{}, fmt.Errorf("%w: decode TUF private key: %v", ErrTUFSigning, err)
	}
	switch len(decoded) {
	case ed25519.SeedSize:
		privateKey := ed25519.NewKeyFromSeed(decoded)
		return TUFSigner{KeyID: clean(keyID), PrivateKey: privateKey, PublicKey: privateKey.Public().(ed25519.PublicKey)}, nil
	case ed25519.PrivateKeySize:
		privateKey := ed25519.PrivateKey(decoded)
		return TUFSigner{KeyID: clean(keyID), PrivateKey: privateKey, PublicKey: privateKey.Public().(ed25519.PublicKey)}, nil
	default:
		return TUFSigner{}, fmt.Errorf("%w: TUF private key must be an ed25519 seed or private key", ErrTUFSigning)
	}
}

func (s TUFSigner) Ready() error {
	if s.KeyID == "" {
		return fmt.Errorf("%w: TUF key id is required", ErrTUFSigning)
	}
	if len(s.PrivateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("%w: TUF ed25519 private key is required", ErrTUFSigning)
	}
	return nil
}

func (s TUFSigner) buildSet(packageName string, artifact Artifact, versions tufVersions, now time.Time) ([]TUFMetadata, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	targetsRole, err := s.sign(targetsSigned{
		Type:        "targets",
		SpecVersion: "1.0.34",
		Version:     versions.Targets,
		Expires:     now.Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339),
		Targets: map[string]targetFile{
			targetPath(artifact): {
				Length: artifact.OCISizeBytes,
				Hashes: map[string]string{"sha256": digestHex(artifact.OCIDigest)},
				Custom: targetCustom{
					OCIReference: artifact.PublicOCIReference,
					Package:      artifact.PackageName,
					Version:      artifact.PackageVersion,
					Channel:      artifact.ChannelName,
					PlatformOS:   artifact.PlatformOS,
					PlatformArch: artifact.PlatformArch,
					SourceCommit: artifact.SourceCommit,
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	targetsHash := sha256Digest(targetsRole)
	snapshotRole, err := s.sign(snapshotSigned{
		Type:        "snapshot",
		SpecVersion: "1.0.34",
		Version:     versions.Snapshot,
		Expires:     now.Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339),
		Meta: map[string]metaFile{
			"targets.json": {Version: versions.Targets, Length: int64(len(targetsRole)), Hashes: map[string]string{"sha256": digestHex(targetsHash)}},
		},
	})
	if err != nil {
		return nil, err
	}
	snapshotHash := sha256Digest(snapshotRole)
	timestampRole, err := s.sign(timestampSigned{
		Type:        "timestamp",
		SpecVersion: "1.0.34",
		Version:     versions.Timestamp,
		Expires:     now.Add(24 * time.Hour).UTC().Format(time.RFC3339),
		Meta: map[string]metaFile{
			"snapshot.json": {Version: versions.Snapshot, Length: int64(len(snapshotRole)), Hashes: map[string]string{"sha256": digestHex(snapshotHash)}},
		},
	})
	if err != nil {
		return nil, err
	}
	rootRole, err := s.sign(rootSigned{
		Type:               "root",
		SpecVersion:        "1.0.34",
		Version:            1,
		Expires:            now.Add(365 * 24 * time.Hour).UTC().Format(time.RFC3339),
		ConsistentSnapshot: true,
		Keys: map[string]keyDescription{
			s.KeyID: {
				KeyType: "ed25519",
				Scheme:  "ed25519",
				KeyVal:  map[string]string{"public": hex.EncodeToString(s.PublicKey)},
			},
		},
		Roles: map[string]roleDescription{
			"root":      {KeyIDs: []string{s.KeyID}, Threshold: 1},
			"targets":   {KeyIDs: []string{s.KeyID}, Threshold: 1},
			"snapshot":  {KeyIDs: []string{s.KeyID}, Threshold: 1},
			"timestamp": {KeyIDs: []string{s.KeyID}, Threshold: 1},
		},
	})
	if err != nil {
		return nil, err
	}
	return []TUFMetadata{
		metadata(packageName, "root", 1, rootRole, now, now.Add(365*24*time.Hour)),
		metadata(packageName, "targets", versions.Targets, targetsRole, now, now.Add(30*24*time.Hour)),
		metadata(packageName, "snapshot", versions.Snapshot, snapshotRole, now, now.Add(7*24*time.Hour)),
		metadata(packageName, "timestamp", versions.Timestamp, timestampRole, now, now.Add(24*time.Hour)),
	}, nil
}

func (s TUFSigner) sign(signed any) ([]byte, error) {
	signedBytes, err := json.Marshal(signed)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal signed TUF body: %v", ErrTUFSigning, err)
	}
	signature := ed25519.Sign(s.PrivateKey, signedBytes)
	body, err := json.Marshal(signedEnvelope{
		Signatures: []signatureRecord{{KeyID: s.KeyID, Signature: hex.EncodeToString(signature)}},
		Signed:     json.RawMessage(signedBytes),
	})
	if err != nil {
		return nil, fmt.Errorf("%w: marshal signed TUF envelope: %v", ErrTUFSigning, err)
	}
	return body, nil
}

func metadata(packageName, role string, version int64, body []byte, signedAt time.Time, expiresAt time.Time) TUFMetadata {
	return TUFMetadata{
		PackageName: packageName,
		Role:        role,
		Version:     version,
		Body:        body,
		BodySHA256:  sha256Digest(body),
		BodyLength:  int64(len(body)),
		SignedAt:    signedAt.UTC(),
		ExpiresAt:   expiresAt.UTC(),
	}
}

func targetPath(artifact Artifact) string {
	return artifact.ChannelName + "/" + artifact.PlatformOS + "/" + artifact.PlatformArch + "/" + artifact.PackageName
}

func sha256Digest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type signedEnvelope struct {
	Signatures []signatureRecord `json:"signatures"`
	Signed     json.RawMessage   `json:"signed"`
}

type signatureRecord struct {
	KeyID     string `json:"keyid"`
	Signature string `json:"sig"`
}

type rootSigned struct {
	Type               string                     `json:"_type"`
	SpecVersion        string                     `json:"spec_version"`
	Version            int64                      `json:"version"`
	Expires            string                     `json:"expires"`
	ConsistentSnapshot bool                       `json:"consistent_snapshot"`
	Keys               map[string]keyDescription  `json:"keys"`
	Roles              map[string]roleDescription `json:"roles"`
}

type keyDescription struct {
	KeyType string            `json:"keytype"`
	Scheme  string            `json:"scheme"`
	KeyVal  map[string]string `json:"keyval"`
}

type roleDescription struct {
	KeyIDs    []string `json:"keyids"`
	Threshold int      `json:"threshold"`
}

type targetsSigned struct {
	Type        string                `json:"_type"`
	SpecVersion string                `json:"spec_version"`
	Version     int64                 `json:"version"`
	Expires     string                `json:"expires"`
	Targets     map[string]targetFile `json:"targets"`
}

type targetFile struct {
	Length int64             `json:"length"`
	Hashes map[string]string `json:"hashes"`
	Custom targetCustom      `json:"custom"`
}

type targetCustom struct {
	OCIReference string `json:"oci_reference"`
	Package      string `json:"package"`
	Version      string `json:"version"`
	Channel      string `json:"channel"`
	PlatformOS   string `json:"platform_os"`
	PlatformArch string `json:"platform_arch"`
	SourceCommit string `json:"source_commit"`
}

type snapshotSigned struct {
	Type        string              `json:"_type"`
	SpecVersion string              `json:"spec_version"`
	Version     int64               `json:"version"`
	Expires     string              `json:"expires"`
	Meta        map[string]metaFile `json:"meta"`
}

type timestampSigned struct {
	Type        string              `json:"_type"`
	SpecVersion string              `json:"spec_version"`
	Version     int64               `json:"version"`
	Expires     string              `json:"expires"`
	Meta        map[string]metaFile `json:"meta"`
}

type metaFile struct {
	Version int64             `json:"version"`
	Length  int64             `json:"length"`
	Hashes  map[string]string `json:"hashes"`
}
