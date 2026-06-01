package client

import "time"

type ArtifactUpload struct {
	Output    string `json:"output"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

type CreateUploadSessionRequest struct {
	Site         string           `json:"site"`
	DeployRunKey string           `json:"deploy_run_key"`
	SHA          string           `json:"sha"`
	Artifacts    []ArtifactUpload `json:"artifacts"`
}

type UploadObject struct {
	Output       string            `json:"output"`
	Bucket       string            `json:"bucket"`
	Key          string            `json:"key"`
	GetterSource string            `json:"getter_source"`
	PutURL       string            `json:"put_url"`
	Headers      map[string]string `json:"headers"`
	ExpiresAt    time.Time         `json:"expires_at"`
}

type CreateUploadSessionResponse struct {
	SessionID string         `json:"session_id"`
	ExpiresAt time.Time      `json:"expires_at"`
	Objects   []UploadObject `json:"objects"`
}

type CompleteUploadSessionResponse struct {
	SessionID   string         `json:"session_id"`
	Site        string         `json:"site"`
	CompletedAt time.Time      `json:"completed_at"`
	Objects     []UploadObject `json:"objects"`
}
