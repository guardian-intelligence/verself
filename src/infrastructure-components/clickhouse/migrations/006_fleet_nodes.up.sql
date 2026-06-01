-- Fleet node capability projection.
--
-- Append-only snapshots of the schedulable fleet keyed by the four placement
-- coordinates: region (cell / fate domain), datacenter (cluster / placement
-- locality), node_pool (role), and meta (arbitrary attested capabilities, e.g.
-- cpu_isa_level). nomad-observer writes one row per node per snapshot tick; current
-- state is the argMax(*, observed_at) per node_id. Rows are never updated.
--
-- `attestation_method` records how the node's identity was proven at boot
-- (join_token today; tpm_devid once the TPM DevID node attestor lands). `meta`
-- carries only capabilities the node's own SVID attests, never self-asserted
-- Nomad client meta.
CREATE TABLE IF NOT EXISTS verself.fleet_nodes
(
    region             LowCardinality(String) CODEC(ZSTD(3)),
    datacenter         LowCardinality(String) CODEC(ZSTD(3)),
    node_pool          LowCardinality(String) CODEC(ZSTD(3)),
    node_class         LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    cpu_isa_level      LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    node_id            String                 CODEC(ZSTD(3)),
    node_name          LowCardinality(String) CODEC(ZSTD(3)),
    spiffe_id          String DEFAULT ''      CODEC(ZSTD(3)),
    attestation_method LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    attested           UInt8 DEFAULT 0        CODEC(T64, ZSTD(3)),
    status             LowCardinality(String) CODEC(ZSTD(3)),
    eligibility        LowCardinality(String) DEFAULT '' CODEC(ZSTD(3)),
    drain              UInt8 DEFAULT 0        CODEC(T64, ZSTD(3)),
    meta               Map(LowCardinality(String), String) CODEC(ZSTD(3)),
    modify_index       UInt64 DEFAULT 0       CODEC(T64, ZSTD(3)),
    observed_at        DateTime64(3, 'UTC') DEFAULT now64(3) CODEC(DoubleDelta, ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(toDate(observed_at))
ORDER BY (region, datacenter, node_pool, node_id, observed_at)
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

CREATE USER IF NOT EXISTS nomad_observer IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/nomad-observer' HOST LOCAL;
ALTER USER nomad_observer IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/nomad-observer' HOST LOCAL;
GRANT INSERT ON verself.fleet_nodes TO nomad_observer;
