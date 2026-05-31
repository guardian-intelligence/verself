package r2control

import (
	"fmt"
	"strings"
	"time"
)

const (
	TemporaryPermissionAdminReadWrite  = "admin-read-write"
	TemporaryPermissionAdminReadOnly   = "admin-read-only"
	TemporaryPermissionObjectReadWrite = "object-read-write"
	TemporaryPermissionObjectReadOnly  = "object-read-only"
)

type TemporaryCredentialRequest struct {
	ParentAccessKeyID string
	Bucket            string
	Permission        string
	TTL               time.Duration
	Prefixes          []string
	Objects           []string
}

type TemporaryCredentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

func (req TemporaryCredentialRequest) validate() (TemporaryCredentialRequest, error) {
	req.ParentAccessKeyID = strings.TrimSpace(req.ParentAccessKeyID)
	req.Bucket = strings.TrimSpace(req.Bucket)
	req.Permission = strings.TrimSpace(req.Permission)
	if req.ParentAccessKeyID == "" {
		return TemporaryCredentialRequest{}, fmt.Errorf("parent access key ID is required")
	}
	if !IsR2BucketName(req.Bucket) {
		return TemporaryCredentialRequest{}, fmt.Errorf("bucket must be a valid lowercase R2 bucket name")
	}
	switch req.Permission {
	case TemporaryPermissionAdminReadWrite, TemporaryPermissionAdminReadOnly, TemporaryPermissionObjectReadWrite, TemporaryPermissionObjectReadOnly:
	default:
		return TemporaryCredentialRequest{}, fmt.Errorf("unsupported temporary R2 permission %q", req.Permission)
	}
	if req.TTL == 0 {
		req.TTL = 15 * time.Minute
	}
	if req.TTL < time.Minute || req.TTL > 7*24*time.Hour {
		return TemporaryCredentialRequest{}, fmt.Errorf("R2 temporary credential TTL must be between 1 minute and 7 days")
	}
	req.Prefixes = cleanPaths(req.Prefixes)
	req.Objects = cleanPaths(req.Objects)
	return req, nil
}

func cleanPaths(paths []string) []string {
	cleaned := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimLeft(strings.TrimSpace(path), "/")
		if path != "" {
			cleaned = append(cleaned, path)
		}
	}
	return cleaned
}
