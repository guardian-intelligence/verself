CREATE USER IF NOT EXISTS otelcol IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/otelcol' HOST LOCAL;
ALTER USER otelcol IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/otelcol' HOST LOCAL;
GRANT INSERT ON default.* TO otelcol;
GRANT SELECT ON default.otel_logs TO otelcol;
GRANT SELECT ON default.otel_traces TO otelcol;
