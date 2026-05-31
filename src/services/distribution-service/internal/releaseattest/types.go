package releaseattest

import (
	"context"
	"time"

	releaseinput "github.com/verself/releaseattest"
)

type Request struct {
	Input              releaseinput.ReleaseInput
	ReleaseInputDigest string
	BuilderID          string
	PolicyID           string
	TPM                TPMEvidence
	CheckedAt          time.Time
}

type TPMEvidence struct {
	AKPublic                []byte
	Quotes                  []Quote
	PCRs                    []PCR
	EventLog                []byte
	ReleasePublicName       string
	ReleasePublicBlob       []byte
	ReleasePublicBlobDigest string
	ReleaseKeyCertification ReleaseKeyCertification
}

type Quote struct {
	Quote     []byte
	Signature []byte
}

type PCR struct {
	Index     int
	Digest    string
	DigestAlg string
}

type ReleaseKeyCertification struct {
	Public            []byte
	CreateData        []byte
	CreateAttestation []byte
	CreateSignature   []byte
}

type Result struct {
	ReleaseInputDigest       string
	AKPublicDigest           string
	PCRPolicyID              string
	TPMReleasePublicName     string
	TPMReleasePublicDigest   string
	VerifiedPCRCount         int
	VerifiedQuoteCount       int
	EventLogReplayed         bool
	OpenBaoAuthorizationHint OpenBaoAuthorization
	CheckedAt                time.Time
}

type OpenBaoAuthorization struct {
	KeyName            string
	TransitMount       string
	ReleaseInputDigest string
	TPMReleaseKeyName  string
}

type OpenBaoAuthorizer interface {
	AuthorizeRelease(context.Context, Result) (OpenBaoAuthorization, error)
}

type StaticOpenBaoAuthorizer struct {
	TransitMount string
	KeyName      string
}

func (a StaticOpenBaoAuthorizer) AuthorizeRelease(_ context.Context, result Result) (OpenBaoAuthorization, error) {
	return OpenBaoAuthorization{
		KeyName:            a.KeyName,
		TransitMount:       a.TransitMount,
		ReleaseInputDigest: result.ReleaseInputDigest,
		TPMReleaseKeyName:  result.TPMReleasePublicName,
	}, nil
}

type PCRPolicy struct {
	ID        string
	BuilderID string
	PCRs      []PCR
}

type TrustedAK struct {
	BuilderID string
}
