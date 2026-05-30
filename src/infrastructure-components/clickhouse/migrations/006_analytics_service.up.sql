CREATE USER IF NOT EXISTS analytics_service IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/analytics-service' HOST LOCAL;
ALTER USER analytics_service IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/analytics-service' HOST LOCAL;

GRANT SELECT, INSERT ON verself.analytics_events TO analytics_service;
GRANT SELECT, INSERT ON verself.analytics_ingest_events TO analytics_service;
GRANT SELECT, INSERT ON verself.analytics_access_events TO analytics_service;
