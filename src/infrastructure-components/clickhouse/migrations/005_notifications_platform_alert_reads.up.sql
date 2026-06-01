CREATE USER IF NOT EXISTS notifications_service IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/notifications-service' HOST LOCAL;
ALTER USER notifications_service IDENTIFIED WITH ssl_certificate SAN 'URI:__VERSELF_SPIFFE_SERVICE_PREFIX__/notifications-service' HOST LOCAL;

GRANT SELECT ON default.otel_traces TO notifications_service;
GRANT SELECT, INSERT ON verself.notification_events TO notifications_service;
GRANT SELECT ON verself.durable_events TO notifications_service;
GRANT SELECT ON verself.golden_vm_events TO notifications_service;
