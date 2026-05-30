CREATE USER IF NOT EXISTS grafana_observer IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/grafana' HOST LOCAL;
ALTER USER grafana_observer IDENTIFIED WITH ssl_certificate SAN 'URI:spiffe://spiffe.verself.sh/svc/grafana' HOST LOCAL;
GRANT SELECT ON default.* TO grafana_observer;
GRANT SELECT ON verself.* TO grafana_observer;
GRANT SELECT ON system.query_log TO grafana_observer;
GRANT SELECT ON system.settings TO grafana_observer;
GRANT SELECT ON system.tables TO grafana_observer;
GRANT SELECT ON system.columns TO grafana_observer;
GRANT SELECT ON system.databases TO grafana_observer;
