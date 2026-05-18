ALTER TABLE iam_browser_sessions
  ADD COLUMN created_client_ip_source TEXT NOT NULL DEFAULT '',
  ADD COLUMN created_edge_peer_ip TEXT NOT NULL DEFAULT '',
  ADD COLUMN created_device_label TEXT NOT NULL DEFAULT '',
  ADD COLUMN created_device_kind TEXT NOT NULL DEFAULT '',
  ADD COLUMN created_browser_name TEXT NOT NULL DEFAULT '',
  ADD COLUMN created_os_name TEXT NOT NULL DEFAULT '',
  ADD COLUMN created_geo_country_code TEXT NOT NULL DEFAULT '',
  ADD COLUMN created_geo_region TEXT NOT NULL DEFAULT '',
  ADD COLUMN created_geo_city TEXT NOT NULL DEFAULT '',
  ADD COLUMN last_seen_client_ip_source TEXT NOT NULL DEFAULT '',
  ADD COLUMN last_seen_edge_peer_ip TEXT NOT NULL DEFAULT '',
  ADD COLUMN last_seen_device_label TEXT NOT NULL DEFAULT '',
  ADD COLUMN last_seen_device_kind TEXT NOT NULL DEFAULT '',
  ADD COLUMN last_seen_browser_name TEXT NOT NULL DEFAULT '',
  ADD COLUMN last_seen_os_name TEXT NOT NULL DEFAULT '',
  ADD COLUMN last_seen_geo_country_code TEXT NOT NULL DEFAULT '',
  ADD COLUMN last_seen_geo_region TEXT NOT NULL DEFAULT '',
  ADD COLUMN last_seen_geo_city TEXT NOT NULL DEFAULT '';

CREATE TABLE iam_browser_session_observations (
  observation_id BIGSERIAL PRIMARY KEY,
  session_hash TEXT NOT NULL REFERENCES iam_browser_sessions(session_hash) ON DELETE CASCADE,
  session_handle TEXT NOT NULL,
  subject TEXT NOT NULL,
  client_ip TEXT NOT NULL,
  client_ip_trusted BOOLEAN NOT NULL DEFAULT false,
  client_ip_source TEXT NOT NULL DEFAULT '',
  edge_peer_ip TEXT NOT NULL DEFAULT '',
  user_agent TEXT NOT NULL DEFAULT '',
  device_label TEXT NOT NULL DEFAULT '',
  device_kind TEXT NOT NULL DEFAULT '',
  browser_name TEXT NOT NULL DEFAULT '',
  os_name TEXT NOT NULL DEFAULT '',
  geo_country_code TEXT NOT NULL DEFAULT '',
  geo_region TEXT NOT NULL DEFAULT '',
  geo_city TEXT NOT NULL DEFAULT '',
  observed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_iam_browser_session_observations_subject_time
  ON iam_browser_session_observations (subject, observed_at DESC);

CREATE INDEX idx_iam_browser_session_observations_session_time
  ON iam_browser_session_observations (session_hash, observed_at DESC);
