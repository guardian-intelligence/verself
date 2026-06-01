CREATE USER IF NOT EXISTS analytics_service IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/analytics-service' HOST LOCAL;
ALTER USER analytics_service IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/analytics-service' HOST LOCAL;
GRANT SELECT, INSERT ON verself.analytics_events TO analytics_service;
GRANT INSERT ON verself.analytics_ingest_events TO analytics_service;
GRANT INSERT ON verself.analytics_access_events TO analytics_service;

CREATE USER IF NOT EXISTS billing_service IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/billing-service' HOST LOCAL;
ALTER USER billing_service IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/billing-service' HOST LOCAL;
GRANT INSERT ON verself.metering TO billing_service;
GRANT INSERT ON verself.billing_events TO billing_service;

CREATE USER IF NOT EXISTS governance_service IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/governance-service' HOST LOCAL;
ALTER USER governance_service IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/governance-service' HOST LOCAL;
GRANT SELECT, INSERT ON verself.api_activity_events TO governance_service;
GRANT SELECT, INSERT ON verself.api_activity_payloads TO governance_service;
GRANT SELECT, INSERT ON verself.api_activity_resources TO governance_service;

CREATE USER IF NOT EXISTS object_storage_service IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/object-storage-service' HOST LOCAL;
ALTER USER object_storage_service IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/object-storage-service' HOST LOCAL;
GRANT INSERT ON verself.object_access_events TO object_storage_service;
