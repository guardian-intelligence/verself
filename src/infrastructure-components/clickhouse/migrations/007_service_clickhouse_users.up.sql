CREATE USER IF NOT EXISTS billing_service IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/billing-service' HOST LOCAL;
ALTER USER billing_service IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/billing-service' HOST LOCAL;
GRANT INSERT ON verself.billing_events TO billing_service;
GRANT INSERT ON verself.metering TO billing_service;

CREATE USER IF NOT EXISTS governance_service IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/governance-service' HOST LOCAL;
ALTER USER governance_service IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/governance-service' HOST LOCAL;
GRANT SELECT, INSERT ON verself.api_activity_events TO governance_service;
GRANT SELECT, INSERT ON verself.api_activity_payloads TO governance_service;
GRANT SELECT, INSERT ON verself.api_activity_resources TO governance_service;

CREATE USER IF NOT EXISTS object_storage_service IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/object-storage-service' HOST LOCAL;
ALTER USER object_storage_service IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/object-storage-service' HOST LOCAL;
GRANT INSERT ON verself.object_access_events TO object_storage_service;
