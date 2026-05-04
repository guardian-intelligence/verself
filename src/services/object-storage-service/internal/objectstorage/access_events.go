package objectstorage

import "time"

type ObjectAccessEvent struct {
	RecordedAt         time.Time `ch:"recorded_at"`
	EventDate          time.Time `ch:"event_date"`
	Environment        string    `ch:"environment"`
	ServiceVersion     string    `ch:"service_version"`
	WriterInstanceID   string    `ch:"writer_instance_id"`
	OrgID              string    `ch:"org_id"`
	BucketID           string    `ch:"bucket_id"`
	BucketName         string    `ch:"bucket_name"`
	RequestedBucket    string    `ch:"requested_bucket"`
	ResolvedAlias      string    `ch:"resolved_alias"`
	ResolvedPrefix     string    `ch:"resolved_prefix"`
	Operation          string    `ch:"operation"`
	AuthMode           string    `ch:"auth_mode"`
	AccessKeyID        string    `ch:"access_key_id"`
	SPIFFEPeerID       string    `ch:"spiffe_peer_id"`
	TraceID            string    `ch:"trace_id"`
	SpanID             string    `ch:"span_id"`
	Status             uint16    `ch:"status"`
	BytesIn            uint64    `ch:"bytes_in"`
	BytesOut           uint64    `ch:"bytes_out"`
	LatencyMS          uint32    `ch:"latency_ms"`
	ClientIPHash       string    `ch:"client_ip_hash"`
	UserAgentHash      string    `ch:"user_agent_hash"`
	ErrorClass         string    `ch:"error_class"`
	ErrorMessage       string    `ch:"error_message"`
	UpstreamStatus     uint16    `ch:"upstream_status"`
	UpstreamRequestURI string    `ch:"upstream_request_uri"`
}
