package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type DataRightsRequest struct {
	RequestID   string
	RequestedAt time.Time
	RequestedBy string
	Traceparent string
	OrgID       string
	SubjectID   string
}

type DataRightsManifest struct {
	RequestID          string
	RequestType        string
	Status             string
	OrgID              string
	SubjectID          string
	Artifacts          []DataRightsArtifact
	ErasureActions     []DataRightsErasureAction
	RetainedCategories []DataRightsRetainedCategory
	RecordCounts       map[string]string
	CompletedAt        time.Time
}

type DataRightsArtifact struct {
	Path        string
	ContentType string
	Rows        string
	Bytes       string
	SHA256      string
}

type DataRightsErasureAction struct {
	Name        string
	Rows        string
	Description string
}

type DataRightsRetainedCategory struct {
	Category string
	Reason   string
}

func (r DataRightsRequest) validate(kind string) error {
	if strings.TrimSpace(r.RequestID) == "" {
		return fmt.Errorf("%w: request_id is required", ErrInvalidInput)
	}
	if strings.TrimSpace(r.RequestedBy) == "" {
		return fmt.Errorf("%w: requested_by is required", ErrInvalidInput)
	}
	switch kind {
	case "org_export":
		if strings.TrimSpace(r.OrgID) == "" {
			return fmt.Errorf("%w: org_id is required", ErrInvalidInput)
		}
	case "subject_export", "subject_erasure":
		if strings.TrimSpace(r.SubjectID) == "" {
			return fmt.Errorf("%w: subject_id is required", ErrInvalidInput)
		}
	default:
		return fmt.Errorf("%w: unsupported data rights request type", ErrInvalidInput)
	}
	return nil
}

func artifactFor(path string, rows int, content []byte) DataRightsArtifact {
	sum := sha256.Sum256(content)
	return DataRightsArtifact{
		Path:        path,
		ContentType: "application/jsonl",
		Rows:        strconv.Itoa(rows),
		Bytes:       strconv.Itoa(len(content)),
		SHA256:      hex.EncodeToString(sum[:]),
	}
}
