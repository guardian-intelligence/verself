package zfs

// User-property keys vm-orchestrator stamps on datasets and snapshots. The
// "vs:" prefix namespaces them so they sort with other Verself metadata in
// `zfs get all` output and don't collide with operator-applied properties.
const (
	PropLeaseID             = "vs:lease_id"
	PropCreatedAt           = "vs:created_at"
	PropCommittedAt         = "vs:committed_at"
	PropFilesystemMount     = "vs:filesystem_mount"
	PropFilesystemTargetRef = "vs:filesystem_target_ref"
	PropFilesystemSourceRef = "vs:filesystem_source_ref"
	PropCheckpointRef       = "vs:checkpoint_ref"
	PropCheckpointVersion   = "vs:checkpoint_version"
	PropCheckpointCreated   = "vs:checkpoint_created"
	PropCheckpointOperation = "vs:checkpoint_operation"

	// Seed properties stamped on the @ready snapshot of a composable image
	// during VolumeLifecycle.Seed. PropSourceDigest is the idempotency key:
	// re-running Seed with the same digest emits SeedOutcomeUpToDate and no
	// destructive ops.
	PropImageRef       = "vs:image_ref"
	PropSourceDigest   = "vs:source_digest"
	PropSeededAt       = "vs:seeded_at"
	PropSeedStrategy   = "vs:seed_strategy"
	PropSeedSizeBytes  = "vs:seed_size_bytes"
	PropSeedSeededFrom = "vs:seed_seeded_from"
)
